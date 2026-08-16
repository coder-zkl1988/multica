import { resolve } from "path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

type BundleOutput =
  | { type: "asset" }
  | {
      type: "chunk";
      imports: string[];
      dynamicImports: string[];
    };

export function workspaceExternalGuard() {
  return {
    name: "multica:workspace-external-guard",
    generateBundle(
      _options: unknown,
      bundle: Record<string, BundleOutput>,
    ) {
      const externalWorkspaceImports = [
        ...new Set(
          Object.values(bundle).flatMap((output) =>
            output.type === "chunk"
              ? [...output.imports, ...output.dynamicImports].filter((specifier) =>
                  specifier.startsWith("@multica/"),
                )
              : [],
          ),
        ),
      ];

      if (externalWorkspaceImports.length > 0) {
        throw new Error(
          `desktop main bundle externalized source-only workspace packages: ${externalWorkspaceImports.join(", ")}`,
        );
      }
    },
  };
}

export default defineConfig({
  main: {
    plugins: [
      externalizeDepsPlugin({
        exclude: ["@multica/core", "@multica/ui", "@multica/views"],
      }),
      workspaceExternalGuard(),
    ],
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: {
    server: {
      // Allow parallel worktrees to run `pnpm dev:desktop` side-by-side
      // (e.g. Multica Canary alongside a primary checkout) by overriding
      // the renderer port via env. Falls back to 5173 for the common case.
      port: Number(process.env.DESKTOP_RENDERER_PORT) || 5173,
      strictPort: true,
    },
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": resolve("src/renderer/src"),
      },
      dedupe: ["react", "react-dom", "@tanstack/react-query"],
    },
  },
});
