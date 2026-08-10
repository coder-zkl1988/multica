import reactConfig from "@multica/eslint-config/react";
import i18next from "eslint-plugin-i18next";

// Global i18n protection. Every JSX text node in this package must pass
// through useT() — raw strings become a build error. Scope of
// `mode: "jsx-text-only"`: flags raw strings inside JSX children only;
// attribute values and plain TS literals are allowed through.

export default [
  ...reactConfig,
  {
    files: ["**/*.tsx"],
    ignores: ["**/*.test.tsx", "test/**"],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": [
        "error",
        { mode: "jsx-text-only" },
      ],
    },
  },
  {
    // The design workbench is currently a Chinese-only internal surface. Keep
    // raw copy visible in CI without blocking unrelated releases until the
    // design namespace is added to all four locale bundles.
    files: [
      "designs/**/*.tsx",
      "issues/components/issue-design-restore-section.tsx",
    ],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": "warn",
    },
  },
];
