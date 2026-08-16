import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export function findExternalWorkspaceImports(bundle) {
  return [
    ...new Set(
      [...bundle.matchAll(/require\(["'](@multica\/[^"']+)["']\)/g)].map(
        (match) => match[1],
      ),
    ),
  ];
}

export function verifyMainBundle(
  bundlePath = resolve(
    dirname(fileURLToPath(import.meta.url)),
    "../out/main/index.js",
  ),
) {
  const externalWorkspaceImports = findExternalWorkspaceImports(
    readFileSync(bundlePath, "utf8"),
  );

  if (externalWorkspaceImports.length > 0) {
    throw new Error(
      `desktop main bundle externalized source-only workspace packages: ${externalWorkspaceImports.join(", ")}`,
    );
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  verifyMainBundle();
}
