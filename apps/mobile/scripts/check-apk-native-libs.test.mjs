import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { crc32, deflateRawSync } from "node:zlib";
import test from "node:test";

import { findClosureProblems, findNdk, findZipalign, parseReadelfJson, readZipEntries } from "./check-apk-native-libs.mjs";

const SCRIPT = fileURLToPath(new URL("./check-apk-native-libs.mjs", import.meta.url));

// Writes a zip with no alignment padding, so stored .so entries land at
// arbitrary offsets, as an unaligned APK would.
function writeZip(path, files) {
  const locals = [];
  const central = [];
  let offset = 0;
  for (const { name, data, deflate = false } of files) {
    const nameBytes = Buffer.from(name);
    const body = deflate ? deflateRawSync(data) : data;
    const method = deflate ? 8 : 0;
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(method, 8);
    local.writeUInt32LE(crc32(data), 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(nameBytes.length, 26);
    const header = Buffer.alloc(46);
    header.writeUInt32LE(0x02014b50, 0);
    header.writeUInt16LE(20, 4);
    header.writeUInt16LE(20, 6);
    header.writeUInt16LE(method, 10);
    header.writeUInt32LE(crc32(data), 16);
    header.writeUInt32LE(body.length, 20);
    header.writeUInt32LE(data.length, 24);
    header.writeUInt16LE(nameBytes.length, 28);
    header.writeUInt32LE(offset, 42);
    locals.push(local, nameBytes, body);
    central.push(header, nameBytes);
    offset += local.length + nameBytes.length + body.length;
  }
  const directory = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(files.length, 8);
  end.writeUInt16LE(files.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  writeFileSync(path, Buffer.concat([...locals, directory, end]));
}

const lib = (needed, imports = [], exports = []) => ({
  needed,
  imports: new Set(imports),
  exports: new Set(exports),
  loadAlignments: [],
});

test("reads stored and deflated zip entries, filtered by name", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "apk-native-libs-zip-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const zip = join(dir, "app.apk");
  writeZip(zip, [
    { name: "AndroidManifest.xml", data: Buffer.from("manifest") },
    { name: "lib/arm64-v8a/liba.so", data: Buffer.from("stored bytes") },
    { name: "lib/arm64-v8a/libb.so", data: Buffer.from("deflated bytes ".repeat(50)), deflate: true },
  ]);

  const entries = readZipEntries(readFileSync(zip), (name) => name.startsWith("lib/"));

  assert.deepEqual([...entries.keys()], ["lib/arm64-v8a/liba.so", "lib/arm64-v8a/libb.so"]);
  assert.equal(entries.get("lib/arm64-v8a/liba.so").toString(), "stored bytes");
  assert.equal(entries.get("lib/arm64-v8a/libb.so").toString(), "deflated bytes ".repeat(50));
});

test("keeps global imports and default/protected exports, drops versions, weak imports and hidden symbols", () => {
  const symbol = (name, binding, section, visibility = 0) => ({
    Symbol: { Name: { Name: name }, Binding: { Name: binding }, Section: { Name: section }, Other: { Value: visibility, Flags: [] } },
  });
  const parsed = parseReadelfJson(
    JSON.stringify([
      {
        NeededLibraries: ["libc++_shared.so", "libc.so"],
        DynamicSymbols: [
          symbol("", "Local", "Undefined"),
          symbol("__cxa_finalize@LIBC", "Global", "Undefined"),
          symbol("__cxa_init_primary_exception", "Global", "Undefined"),
          symbol("optional_hook", "Weak", "Undefined"),
          symbol("JNI_OnLoad", "Global", ".text"),
          symbol("protected_fn@@V1", "Global", ".text", 3),
          symbol("weak_default", "Weak", ".text"),
          symbol("hidden_fn", "Global", ".text", 2),
        ],
        ProgramHeaders: [
          { ProgramHeader: { Type: { Name: "PT_PHDR" }, Alignment: 8 } },
          { ProgramHeader: { Type: { Name: "PT_LOAD" }, Alignment: 0x4000 } },
          { ProgramHeader: { Type: { Name: "PT_LOAD" }, Alignment: 0x1000 } },
        ],
      },
    ]),
  );

  assert.deepEqual(parsed.needed, ["libc++_shared.so", "libc.so"]);
  assert.deepEqual([...parsed.imports].sort(), ["__cxa_finalize", "__cxa_init_primary_exception"]);
  assert.deepEqual([...parsed.exports].sort(), ["JNI_OnLoad", "protected_fn", "weak_default"]);
  assert.deepEqual(parsed.loadAlignments, [0x4000, 0x1000]);
});

test("resolves imports through the whole DT_NEEDED tree and reports what is left", () => {
  const packaged = new Map([
    ["libapp.so", lib(["libmid.so"], ["deep_fn", "gone_fn", "malloc"])],
    ["libmid.so", lib(["libdeep.so", "libc.so"], [], ["mid_fn"])],
    ["libdeep.so", lib([], [], ["deep_fn"])],
    ["liborphan.so", lib(["libnowhere.so"], ["mid_fn"])],
  ]);
  const systemLib = (soname) => (soname === "libc.so" ? lib([], [], ["malloc"]) : null);

  assert.deepEqual(findClosureProblems(packaged, systemLib), [
    { kind: "unresolved-symbols", lib: "libapp.so", needed: ["libmid.so"], symbols: ["gone_fn"] },
    { kind: "missing-library", lib: "liborphan.so", dependency: "libnowhere.so" },
    { kind: "unresolved-symbols", lib: "liborphan.so", needed: ["libnowhere.so"], symbols: ["mid_fn"] },
  ]);
});

// End-to-end against real ELF files built with the NDK. CI sets
// REQUIRE_ANDROID_TOOLCHAIN=1 so a missing NDK fails instead of skipping.
function toolchain() {
  try {
    return { tools: { ...findNdk(undefined), zipalign: findZipalign(undefined) }, skip: false };
  } catch (error) {
    if (process.env.REQUIRE_ANDROID_TOOLCHAIN === "1") throw error;
    return { tools: null, skip: `Android toolchain unavailable: ${error.message}` };
  }
}

const { tools, skip } = toolchain();

test("negative control: flags the fbjni 0.8.1 / NDK r27.1 libc++ skew and 4 KB pages", { skip }, (t) => {
  const dir = mkdtempSync(join(tmpdir(), "apk-native-libs-e2e-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const clang = join(tools.prebuilt, "bin/clang");
  const build = (out, soname, source, { pageSize = 16384, link = [] } = {}) => {
    const src = join(dir, `${soname}.c`);
    writeFileSync(src, source);
    mkdirSync(join(out, ".."), { recursive: true });
    execFileSync(clang, [
      "--target=aarch64-linux-android24", "-shared", "-fPIC", "-o", out, src,
      `-Wl,-soname,${soname}`, `-Wl,-z,max-page-size=${pageSize}`, ...link,
    ]);
    return out;
  };

  // libc++_shared.so before and after it gained __cxa_init_primary_exception,
  // and a libfbjni.so built against the newer one, as fbjni 0.8.1 was.
  const oldCxx = build(join(dir, "old/libc++_shared.so"), "libc++_shared.so", "void *__cxa_allocate_exception(unsigned long n) { return 0; }\n");
  const newCxx = build(
    join(dir, "new/libc++_shared.so"),
    "libc++_shared.so",
    "void *__cxa_allocate_exception(unsigned long n) { return 0; }\nvoid *__cxa_init_primary_exception(void *e, void *t, void (*d)(void *)) { return e; }\n",
  );
  const fbjni = build(
    join(dir, "libfbjni.so"),
    "libfbjni.so",
    [
      "void *__cxa_allocate_exception(unsigned long n);",
      "void *__cxa_init_primary_exception(void *e, void *t, void (*d)(void *));",
      "extern void fbjni_optional_hook(void) __attribute__((weak));",
      "void *fbjni_throw(void) {",
      "  if (fbjni_optional_hook) fbjni_optional_hook();",
      "  return __cxa_init_primary_exception(__cxa_allocate_exception(8), 0, 0);",
      "}",
      "",
    ].join("\n"),
    { link: [newCxx] },
  );
  const fourKb = build(join(dir, "libfourkb.so"), "libfourkb.so", "int fourkb(void) { return 4; }\n", { pageSize: 4096 });

  const apk = (name, libs, { align = true } = {}) => {
    const raw = join(dir, `${name}.raw.zip`);
    const files = Object.entries(libs).map(([soname, path]) => ({ name: `lib/arm64-v8a/${soname}`, data: readFileSync(path) }));
    writeZip(raw, [{ name: "AndroidManifest.xml", data: Buffer.from("manifest"), deflate: true }, ...files]);
    if (!align) return raw;
    const aligned = join(dir, `${name}.apk`);
    execFileSync(tools.zipalign, ["-f", "-P", "16", "4", raw, aligned]);
    return aligned;
  };
  const check = (path) => {
    const result = spawnSync(process.execPath, [SCRIPT, path, "--ndk", tools.ndk, "--zipalign", tools.zipalign], {
      encoding: "utf8",
      env: { ...process.env, GITHUB_ACTIONS: "" },
    });
    return { status: result.status, output: `${result.stdout}${result.stderr}` };
  };

  const good = check(apk("good", { "libfbjni.so": fbjni, "libc++_shared.so": newCxx }));
  assert.equal(good.status, 0, good.output);
  assert.match(good.output, /ABI closure: all \d+ global imports resolve/);

  const skew = check(apk("skew", { "libfbjni.so": fbjni, "libc++_shared.so": oldCxx }));
  assert.equal(skew.status, 1, skew.output);
  assert.match(skew.output, /libfbjni\.so imports 1 symbol\(s\)[^\n]*\n\s+__cxa_init_primary_exception\n/);
  assert.match(skew.output, /1 native-lib problem\(s\)/);

  const pages = check(apk("pages", { "libfbjni.so": fbjni, "libc++_shared.so": newCxx, "libfourkb.so": fourKb }));
  assert.equal(pages.status, 1, pages.output);
  assert.match(pages.output, /libfourkb\.so has LOAD segments aligned to 0x1000/);

  const unaligned = check(apk("unaligned", { "libfbjni.so": fbjni, "libc++_shared.so": newCxx }, { align: false }));
  assert.equal(unaligned.status, 1, unaligned.output);
  assert.match(unaligned.output, /zipalign -c -P 16 -v 4 rejected the APK/);
  assert.match(unaligned.output, /1 native-lib problem\(s\)/);
});
