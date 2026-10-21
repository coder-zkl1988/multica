const fs = require("fs");
const path = require("path");
const { withProjectBuildGradle } = require("expo/config-plugins");

/**
 * Pins floating fbjni requests to the version React Native ships.
 *
 * `react-native-shiki-engine` declares `implementation "com.facebook.fbjni:fbjni:+"`.
 * A `+` resolves to the newest fbjni on Maven Central, and once 0.8.1 was
 * published it outranked the 0.7.0 that `react-android` and `hermes-android`
 * both require. Gradle's conflict resolution then picks 0.8.1, so the APK gets
 * fbjni 0.8.1's `libfbjni.so`, which imports `__cxa_init_primary_exception`.
 * The packaged `libc++_shared.so` comes from the app's own CMake build on
 * NDK r27.1, and it does not export that symbol, so `libfbjni.so` cannot be
 * loaded. The app dies inside SoLoader in MainApplication.onCreate, before
 * it ever asks Metro for a bundle, and there is no JS-side trace.
 *
 * The crash signature depends on how native libs are packaged:
 *   - default prebuild (`expo.useLegacyPackaging=false`, libs loaded straight
 *     from the APK): `SoLoaderDSONotFoundError: couldn't find DSO to load:
 *     libfbjni.so`, thrown from DirectApkSoSource.loadDependencies;
 *   - with `expo.useLegacyPackaging=true` (libs extracted at install):
 *     `UnsatisfiedLinkError: cannot locate symbol "__cxa_init_primary_exception"
 *     referenced by libfbjni.so`.
 *
 * Because the cause is a floating version, the breakage tracks what Maven
 * Central has published, not the Expo SDK. Only Android loads these libs, and
 * only this fork builds Android (upstream has no Android build), so upstream
 * never hits it.
 *
 * The rule rewrites only dynamic selectors (`+` prefixes, `latest.*`, version
 * ranges) and leaves explicit requests to normal conflict resolution, so a
 * future React Native that moves to fbjni 0.8.x is not silently downgraded.
 * The fbjni headers and `libfbjni.so` the APK gets are decided by :app's
 * resolved graph alone: shiki-engine's C++ is not built in its own module, and
 * autolinking adds it to :app's CMake (`cxxModuleCMakeListsPath`). The rule is
 * declared under `allprojects` so that each module's own configurations agree
 * too, notably shiki-engine's JVM classpaths, where the `fbjni:+` lives. That
 * also means `+` is never resolved against Maven.
 *
 * The block is stripped and rewritten on every prebuild, so the version always
 * matches the React Native in node_modules, even when `android/` is reused
 * rather than regenerated.
 *
 * Verified so far: on a HONOR PTP-AN10 (Android 17, arm64-v8a), a debug build
 * with fbjni held at 0.7.0 and `useLegacyPackaging=true` launched, and login,
 * the workspace list and issue detail worked. The default-packaging build this
 * plugin produces has been checked statically: every global import of all 30
 * arm64 native libs resolves, and all of them are 16 KB-aligned.
 *
 * This is a config plugin, not an edit under `android/`, because `android/`
 * is gitignored prebuild output. The fork's release job runs that prebuild
 * too.
 *
 * Drop the pin when no requester uses a dynamic fbjni selector anymore. To
 * check, run this with the pin in place:
 * `./gradlew :app:dependencyInsight --configuration debugRuntimeClasspath --dependency fbjni`
 * If the output no longer shows the selection reason `Selected by rule:
 * floating fbjni request pinned`, the rule has stopped firing and can go.
 */
const BEGIN = "// MULTICA_FBJNI_PIN BEGIN";
const END = "// MULTICA_FBJNI_PIN END";

function reactNativeFbjniVersion(projectRoot) {
  const rnPackage = require.resolve("react-native/package.json", {
    paths: [projectRoot],
  });
  const catalog = path.join(path.dirname(rnPackage), "gradle", "libs.versions.toml");
  const toml = fs.readFileSync(catalog, "utf8");
  const versions = toml.split(/^\[/m).find((section) => section.startsWith("versions]"));
  const match = versions && versions.match(/^fbjni\s*=\s*"([^"]+)"/m);
  if (!match) {
    throw new Error(
      `with-fbjni-pin: no fbjni version under [versions] in ${catalog}; ` +
        "React Native changed how it declares fbjni — re-check this plugin",
    );
  }
  return match[1];
}

function pinBlock(version) {
  // Gradle's dynamic selectors: a `+` prefix selector ends with `+`; ranges
  // open with `[`, `]` or `(` and contain a comma; `latest.*` is a status.
  // A static version with `+` build metadata (`0.8.1+build3`) is exact.
  return `${BEGIN} — managed by apps/mobile/plugins/with-fbjni-pin.js and
// rewritten on every \`expo prebuild\`; do not edit here. See that plugin for why.
allprojects {
    configurations.configureEach {
        resolutionStrategy.eachDependency { details ->
            if (details.requested.group == 'com.facebook.fbjni' && details.requested.name == 'fbjni') {
                def v = details.requested.version?.trim()
                def isRange = v && (v.startsWith('[') || v.startsWith(']') || v.startsWith('(')) && v.contains(',')
                if (v && (v.endsWith('+') || v.startsWith('latest.') || isRange)) {
                    details.useVersion('${version}')
                    details.because('floating fbjni request pinned to the version React Native ships (plugins/with-fbjni-pin.js)')
                }
            }
        }
    }
}
${END}
`;
}

function stripPinBlock(contents) {
  const start = contents.indexOf(BEGIN);
  if (start === -1) {
    return contents;
  }
  const end = contents.indexOf(END, start);
  if (end === -1) {
    throw new Error(
      "with-fbjni-pin: android/build.gradle has the BEGIN marker without END; " +
        "regenerate it with `npx expo prebuild -p android --clean`",
    );
  }
  return contents.slice(0, start).replace(/\n+$/, "\n") + contents.slice(end + END.length).replace(/^\n+/, "");
}

function applyPin(contents, version) {
  const base = stripPinBlock(contents).replace(/\n*$/, "\n");
  return `${base}\n${pinBlock(version)}`;
}

function withFbjniPin(config) {
  return withProjectBuildGradle(config, (cfg) => {
    if (cfg.modResults.language !== "groovy") {
      throw new Error(
        `with-fbjni-pin expects the root build.gradle to be Groovy, got ${cfg.modResults.language}`,
      );
    }
    const version = reactNativeFbjniVersion(cfg.modRequest.projectRoot);
    cfg.modResults.contents = applyPin(cfg.modResults.contents, version);
    return cfg;
  });
}

module.exports = withFbjniPin;
// Exposed for the plugin's own test; config plugins are called with `config` only.
module.exports.applyPin = applyPin;
