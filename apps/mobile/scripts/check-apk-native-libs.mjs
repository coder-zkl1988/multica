#!/usr/bin/env node
// Fails when an APK packages native libraries that cannot load on a phone.
//
// A native-lib mismatch builds a perfectly good APK. When react-native-shiki-
// engine's floating `fbjni:+` resolved to fbjni 0.8.1, its libfbjni.so imported
// `__cxa_init_primary_exception`, which the NDK r27.1 libc++_shared.so packaged
// next to it does not export. Gradle succeeded, and the app died in SoLoader
// before any JS loaded (see plugins/with-fbjni-pin.js). This script reads the
// packaged libraries themselves, so the next mismatch fails CI instead.
//
// Checks, for every lib/<abi>/*.so in the APK:
//   1. ABI closure: every GLOBAL undefined dynamic symbol is exported by a
//      library in its DT_NEEDED tree. A name in that tree resolves to a
//      library packaged in the APK, else to the NDK sysroot stub for a system
//      library (libc, libm, libdl, liblog, libandroid…) at the highest API
//      level that ships it. The tree is followed transitively because bionic
//      resolves against the whole load group. A dependency that is neither
//      packaged nor a system library is an error: it can never be found.
//      WEAK undefined symbols may stay unresolved, as dlopen allows.
//   2. 16 KB pages: every PT_LOAD segment is aligned to at least 0x4000, and
//      `zipalign -c -P 16 -v 4` accepts the APK.
//
// Usage:
//   node scripts/check-apk-native-libs.mjs <apk> [--abi arm64-v8a] [--ndk <dir>] [--zipalign <path>]
//
// The NDK comes from --ndk, else $ANDROID_NDK_HOME / $ANDROID_NDK_ROOT /
// $ANDROID_NDK, else the newest version under <sdk>/ndk. zipalign comes from
// --zipalign, else the newest <sdk>/build-tools that supports -P (35+). The
// SDK is $ANDROID_HOME, $ANDROID_SDK_ROOT, ~/Library/Android/sdk or
// ~/Android/Sdk. The NDK only supplies llvm-readelf and the system-library
// stubs, so it need not be the exact version the APK was built with.
//
// Exit status: 0 when every check passes, 1 when a check fails, 2 when the
// checks could not run (bad arguments, missing tools, unreadable APK).

import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";
import { inflateRawSync } from "node:zlib";

const MIN_LOAD_ALIGN = 0x4000;
const ABI_TRIPLES = {
  "arm64-v8a": "aarch64-linux-android",
  "armeabi-v7a": "arm-linux-androideabi",
  x86: "i686-linux-android",
  x86_64: "x86_64-linux-android",
};

export class ToolError extends Error {}

// Returns the entries whose name satisfies `wanted`, as name -> contents.
// APKs are plain zips; ZIP64 never comes up below 4 GB and 65535 entries.
export function readZipEntries(zip, wanted) {
  // The end-of-central-directory record is the last 22 bytes plus its comment.
  let eocd = zip.length - 22;
  while (eocd >= Math.max(0, zip.length - 22 - 0xffff)) {
    if (zip.readUInt32LE(eocd) === 0x06054b50 && eocd + 22 + zip.readUInt16LE(eocd + 20) === zip.length) break;
    eocd--;
  }
  if (eocd < Math.max(0, zip.length - 22 - 0xffff)) throw new ToolError("not a zip archive");
  const count = zip.readUInt16LE(eocd + 10);
  let at = zip.readUInt32LE(eocd + 16);
  if (count === 0xffff || at === 0xffffffff) throw new ToolError("ZIP64 archives are not supported");

  const entries = new Map();
  for (let i = 0; i < count; i++) {
    if (zip.readUInt32LE(at) !== 0x02014b50) throw new ToolError("corrupt zip central directory");
    const flags = zip.readUInt16LE(at + 8);
    const method = zip.readUInt16LE(at + 10);
    const size = zip.readUInt32LE(at + 20);
    const nameLength = zip.readUInt16LE(at + 28);
    const next = at + 46 + nameLength + zip.readUInt16LE(at + 30) + zip.readUInt16LE(at + 32);
    const name = zip.toString("utf8", at + 46, at + 46 + nameLength);
    const local = zip.readUInt32LE(at + 42);
    at = next;
    if (!wanted(name)) continue;

    if (flags & 1) throw new ToolError(`${name} is encrypted`);
    if (zip.readUInt32LE(local) !== 0x04034b50) throw new ToolError(`corrupt zip local header for ${name}`);
    const start = local + 30 + zip.readUInt16LE(local + 26) + zip.readUInt16LE(local + 28);
    const raw = zip.subarray(start, start + size);
    if (method === 0) entries.set(name, raw);
    else if (method === 8) entries.set(name, inflateRawSync(raw));
    else throw new ToolError(`${name} uses unsupported zip compression method ${method}`);
  }
  return entries;
}

// Reduces `llvm-readelf --elf-output-style=JSON --dyn-syms --needed-libs
// --program-headers` output to what the checks need.
export function parseReadelfJson(text) {
  const [file] = JSON.parse(text);
  const imports = new Set();
  const exports = new Set();
  for (const { Symbol: sym } of file.DynamicSymbols ?? []) {
    const name = sym.Name.Name.split("@")[0];
    if (!name) continue;
    const binding = sym.Binding.Name;
    if (sym.Section.Name === "Undefined") {
      if (binding === "Global") imports.add(name);
      continue;
    }
    // st_other & 3 is the visibility: DEFAULT (0) and PROTECTED (3) export.
    const visibility = sym.Other.Value & 3;
    if (binding !== "Local" && (visibility === 0 || visibility === 3)) exports.add(name);
  }
  const loadAlignments = (file.ProgramHeaders ?? [])
    .map(({ ProgramHeader: header }) => header)
    .filter((header) => header.Type.Name === "PT_LOAD")
    .map((header) => header.Alignment);
  return { needed: file.NeededLibraries ?? [], imports, exports, loadAlignments };
}

// `packaged` maps soname -> parsed lib; `systemLib(soname)` returns the parsed
// NDK stub, or null when the NDK has no such system library.
export function findClosureProblems(packaged, systemLib) {
  const resolved = new Map();
  const resolve = (soname) => {
    if (!resolved.has(soname)) resolved.set(soname, packaged.get(soname) ?? systemLib(soname));
    return resolved.get(soname);
  };

  const problems = [];
  for (const [soname, lib] of [...packaged].sort(([a], [b]) => a.localeCompare(b))) {
    const providers = new Set();
    const seen = new Set([soname]);
    const queue = [];
    for (const dep of lib.needed) {
      if (resolve(dep)) queue.push(dep);
      else problems.push({ kind: "missing-library", lib: soname, dependency: dep });
    }
    // Missing libraries deeper in the tree are reported by their own parent.
    while (queue.length > 0) {
      const dep = queue.shift();
      if (seen.has(dep)) continue;
      seen.add(dep);
      const depLib = resolve(dep);
      if (!depLib) continue;
      for (const name of depLib.exports) providers.add(name);
      queue.push(...depLib.needed);
    }
    const symbols = [...lib.imports].filter((name) => !providers.has(name)).sort();
    if (symbols.length > 0) problems.push({ kind: "unresolved-symbols", lib: soname, needed: lib.needed, symbols });
  }
  return problems;
}

export function findAlignmentProblems(packaged) {
  const problems = [];
  for (const [soname, lib] of [...packaged].sort(([a], [b]) => a.localeCompare(b))) {
    const short = lib.loadAlignments.filter((align) => align < MIN_LOAD_ALIGN);
    if (short.length > 0) problems.push({ kind: "load-alignment", lib: soname, alignments: short });
  }
  return problems;
}

function compareVersions(a, b) {
  const pa = a.split(/[.-]/).map((part) => Number.parseInt(part, 10) || 0);
  const pb = b.split(/[.-]/).map((part) => Number.parseInt(part, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    if ((pa[i] ?? 0) !== (pb[i] ?? 0)) return (pa[i] ?? 0) - (pb[i] ?? 0);
  }
  return 0;
}

function listVersions(dir) {
  if (!existsSync(dir)) return [];
  return readdirSync(dir).filter((name) => /^\d+(\.\d+)*/.test(name)).sort(compareVersions);
}

function sdkRoots(env) {
  const roots = [env.ANDROID_HOME, env.ANDROID_SDK_ROOT, join(homedir(), "Library/Android/sdk"), join(homedir(), "Android/Sdk")];
  return roots.filter((root) => root && existsSync(root));
}

function llvmPrebuilt(ndk) {
  const prebuilt = join(ndk, "toolchains/llvm/prebuilt");
  const host = existsSync(prebuilt) ? readdirSync(prebuilt).find((name) => existsSync(join(prebuilt, name, "bin/llvm-readelf"))) : undefined;
  return host ? join(prebuilt, host) : null;
}

export function findNdk(explicit, env = process.env) {
  const candidates = explicit
    ? [explicit]
    : [env.ANDROID_NDK_HOME, env.ANDROID_NDK_ROOT, env.ANDROID_NDK].filter(Boolean);
  if (!explicit) {
    for (const root of sdkRoots(env)) {
      for (const version of listVersions(join(root, "ndk")).reverse()) candidates.push(join(root, "ndk", version));
    }
  }
  for (const ndk of candidates) {
    const prebuilt = llvmPrebuilt(ndk);
    if (prebuilt) return { ndk, prebuilt };
  }
  throw new ToolError(
    explicit
      ? `no llvm-readelf under ${explicit}/toolchains/llvm/prebuilt`
      : "no Android NDK found; pass --ndk or set ANDROID_NDK_HOME or ANDROID_HOME",
  );
}

export function findZipalign(explicit, env = process.env) {
  if (explicit) {
    if (!existsSync(explicit)) throw new ToolError(`zipalign not found at ${explicit}`);
    return explicit;
  }
  const found = [];
  for (const root of sdkRoots(env)) {
    for (const version of listVersions(join(root, "build-tools"))) {
      const path = join(root, "build-tools", version, "zipalign");
      if (existsSync(path)) found.push({ version, path });
    }
  }
  const newest = found.sort((a, b) => compareVersions(a.version, b.version)).at(-1);
  if (!newest) throw new ToolError("no zipalign found; pass --zipalign or set ANDROID_HOME");
  if (compareVersions(newest.version, "35") < 0) {
    throw new ToolError(`zipalign ${newest.version} predates -P (build-tools 35+): ${newest.path}`);
  }
  return newest.path;
}

function readElf(readelf, path) {
  const result = spawnSync(
    readelf,
    ["--elf-output-style=JSON", "--dyn-syms", "--needed-libs", "--program-headers", path],
    { encoding: "utf8", maxBuffer: 256 * 1024 * 1024 },
  );
  if (result.status !== 0) throw new ToolError(`llvm-readelf failed on ${path}: ${result.stderr || result.error}`);
  return parseReadelfJson(result.stdout);
}

function systemLibResolver(readelf, sysrootLib) {
  const apiLevels = listVersions(sysrootLib).filter((name) => /^\d+$/.test(name)).reverse();
  if (apiLevels.length === 0) throw new ToolError(`no API-level stub directories under ${sysrootLib}`);
  return (soname) => {
    // Stubs only grow with the API level, so the newest one that ships a
    // library exports the most of it.
    const api = apiLevels.find((level) => existsSync(join(sysrootLib, level, soname)));
    return api ? readElf(readelf, join(sysrootLib, api, soname)) : null;
  };
}

function runZipalign(zipalign, apk) {
  const result = spawnSync(zipalign, ["-c", "-P", "16", "-v", "4", apk], { encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
  if (result.error) throw new ToolError(`could not run ${zipalign}: ${result.error.message}`);
  if (result.status === 0) return [];
  const output = `${result.stdout}${result.stderr}`.split("\n").filter((line) => line.trim() && !line.includes("(OK"));
  return [{ kind: "zipalign", output }];
}

export function formatProblem(problem) {
  switch (problem.kind) {
    case "missing-library":
      return `${problem.lib} needs ${problem.dependency}, which is neither packaged in the APK nor an NDK system library`;
    case "unresolved-symbols": {
      const shown = problem.symbols.slice(0, 20).map((name) => `\n      ${name}`).join("");
      const more = problem.symbols.length > 20 ? `\n      … ${problem.symbols.length - 20} more` : "";
      return `${problem.lib} imports ${problem.symbols.length} symbol(s) that nothing in its DT_NEEDED tree exports (needs ${problem.needed.join(", ")}):${shown}${more}`;
    }
    case "load-alignment":
      return `${problem.lib} has LOAD segments aligned to ${problem.alignments.map((align) => `0x${align.toString(16)}`).join(", ")}, below 16 KB (0x4000)`;
    case "zipalign":
      return `zipalign -c -P 16 -v 4 rejected the APK:\n      ${problem.output.slice(-20).join("\n      ")}`;
    default:
      return `unknown problem ${JSON.stringify(problem)}`;
  }
}

export function checkApk({ apk, abi = "arm64-v8a", ndk: ndkOption, zipalign: zipalignOption, env = process.env }) {
  const triple = ABI_TRIPLES[abi];
  if (!triple) throw new ToolError(`unknown ABI ${abi}; expected one of ${Object.keys(ABI_TRIPLES).join(", ")}`);
  if (!existsSync(apk)) throw new ToolError(`APK not found: ${apk}`);
  const { ndk, prebuilt } = findNdk(ndkOption, env);
  const zipalign = findZipalign(zipalignOption, env);
  const readelf = join(prebuilt, "bin/llvm-readelf");

  const prefix = `lib/${abi}/`;
  const entries = readZipEntries(readFileSync(apk), (name) => name.startsWith(prefix) && name.endsWith(".so"));
  if (entries.size === 0) throw new ToolError(`the APK packages no ${prefix}*.so; wrong --abi?`);

  const dir = mkdtempSync(join(tmpdir(), "apk-native-libs-"));
  try {
    const packaged = new Map();
    for (const [name, contents] of entries) {
      const soname = name.slice(prefix.length);
      const path = join(dir, soname.replaceAll("/", "_"));
      writeFileSync(path, contents);
      packaged.set(soname, readElf(readelf, path));
    }
    const problems = [
      ...findClosureProblems(packaged, systemLibResolver(readelf, join(prebuilt, "sysroot/usr/lib", triple))),
      ...findAlignmentProblems(packaged),
      ...runZipalign(zipalign, apk),
    ];
    const libs = [...packaged.values()];
    return {
      problems,
      ndk,
      zipalign,
      libCount: libs.length,
      importCount: libs.reduce((sum, lib) => sum + lib.imports.size, 0),
      loadCount: libs.reduce((sum, lib) => sum + lib.loadAlignments.length, 0),
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const { values, positionals } = parseArgs({
      allowPositionals: true,
      options: { abi: { type: "string" }, ndk: { type: "string" }, zipalign: { type: "string" } },
    });
    if (positionals.length !== 1) throw new ToolError("usage: check-apk-native-libs.mjs <apk> [--abi <abi>] [--ndk <dir>] [--zipalign <path>]");
    const apk = positionals[0];
    const abi = values.abi ?? "arm64-v8a";
    const result = checkApk({ apk, abi, ndk: values.ndk, zipalign: values.zipalign });
    console.log(`Checked ${result.libCount} lib/${abi} libraries in ${apk}`);
    console.log(`  NDK: ${result.ndk}`);
    console.log(`  zipalign: ${result.zipalign}`);
    if (result.problems.length === 0) {
      console.log(`ABI closure: all ${result.importCount} global imports resolve.`);
      console.log(`16 KB pages: all ${result.loadCount} LOAD segments are aligned to 0x4000 or more, and zipalign -c -P 16 passes.`);
    } else {
      for (const problem of result.problems) {
        const message = formatProblem(problem);
        // Workflow commands end at a newline, so the annotation escapes them.
        const annotation = message.replaceAll("%", "%25").replaceAll("\r", "%0D").replaceAll("\n", "%0A");
        console.log(process.env.GITHUB_ACTIONS ? `::error title=APK native libs::${annotation}` : `  ✗ ${message}`);
      }
      console.error(`\n${result.problems.length} native-lib problem(s); this APK would fail to load or install on a phone.`);
      process.exitCode = 1;
    }
  } catch (error) {
    if (!(error instanceof ToolError) && !(error instanceof TypeError && error.code?.startsWith("ERR_PARSE_ARGS"))) throw error;
    console.error(`check-apk-native-libs: ${error.message}`);
    process.exitCode = 2;
  }
}
