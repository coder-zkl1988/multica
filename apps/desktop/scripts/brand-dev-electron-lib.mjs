export function desiredDevElectronBranding(suffix) {
  const normalizedSuffix = suffix
    ?.toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");

  return {
    bundleIdentifier: normalizedSuffix
      ? `ai.multica.desktop.dev.${normalizedSuffix}`
      : "ai.multica.desktop.dev",
    name: suffix ? `Multica Canary ${suffix}` : "Multica Canary",
    protocolName: "Multica Development",
    protocolScheme: "multica",
  };
}

export function launchServicesRegistration(appPath) {
  return {
    executable:
      "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
    args: ["-f", appPath],
  };
}
