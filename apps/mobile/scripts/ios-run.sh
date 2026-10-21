#!/usr/bin/env bash
# Build and run the iOS app, re-applying app.config.ts to ios/ first.
#
# `expo run:ios` only prebuilds when ios/ is missing (ensureNativeProjectAsync
# in @expo/cli): when the directory already exists it returns early and config
# plugins are never re-applied. Everything driven by app.config.ts — app icon,
# bundle identifier, display name, URL scheme, Info.plist permission strings —
# then stays frozen at whatever the first prebuild produced, while the build
# still reports success.
#
# ios/ is gitignored and fully generated, so prebuilding on every run is safe
# and idempotent. --no-install is fine because run:ios installs pods itself
# when Podfile.lock is out of date.
#
# --no-clean is required. Since Expo SDK 57, prebuild deletes and recreates
# ios/ unless --no-clean is passed (earlier SDKs applied changes in place by
# default). A clean prebuild also deletes ios/Pods, and because
# expo-build-properties sets buildReactNativeFromSource, every run would then
# rebuild React Native's pods from source, which is far too slow for the normal
# edit/run loop. In an interactive terminal with uncommitted changes, a clean
# prebuild also stops to ask "Continue with uncommitted changes?". --no-clean
# still rewrites the signing team, bundle identifier, display name and scene
# manifest in place.
#
# APP_ENV and the .env file are supplied by the calling package.json script, so
# prebuild and run resolve the same variant. Arguments are forwarded to
# run:ios only — prebuild takes the same flags for every variant.
set -euo pipefail

pnpm exec expo prebuild -p ios --no-install --no-clean
exec pnpm exec expo run:ios "$@"
