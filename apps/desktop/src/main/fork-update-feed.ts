import { parseXml, type XElement } from "builder-util-runtime";

const DESKTOP_RELEASE_TAG_PREFIX = "desktop-v";

interface FeedConfigurableUpdater {
  setFeedURL(options: { provider: "generic"; url: string }): void;
}

function normalizeRepository(repository: string): string {
  const parts = repository.trim().split("/");
  if (
    parts.length !== 2 ||
    parts.some((part) => !/^[A-Za-z0-9_.-]+$/.test(part))
  ) {
    throw new Error(`Invalid GitHub repository: ${repository}`);
  }
  return parts.join("/");
}

function releaseTagFromEntry(entry: XElement): string | null {
  for (const link of entry.getElements("link")) {
    if (link.attributes?.rel !== "alternate") continue;

    const href = link.attributes.href;
    if (!href) continue;

    try {
      const url = new URL(href);
      if (url.hostname !== "github.com") continue;

      const marker = "/releases/tag/";
      const markerIndex = url.pathname.indexOf(marker);
      if (markerIndex === -1) continue;

      const tag = decodeURIComponent(
        url.pathname.slice(markerIndex + marker.length),
      );
      if (tag.startsWith(DESKTOP_RELEASE_TAG_PREFIX)) return tag;
    } catch {
      // Ignore malformed links and keep looking for a valid Release entry.
    }
  }
  return null;
}

export function latestDesktopReleaseTagFromAtom(atomXml: string): string | null {
  const feed = parseXml(atomXml);
  if (feed.name !== "feed") {
    throw new Error(`Expected an Atom feed, received <${feed.name}>`);
  }

  for (const entry of feed.getElements("entry")) {
    const tag = releaseTagFromEntry(entry);
    if (tag) return tag;
  }
  return null;
}

export async function configureForkUpdateFeed(
  updater: FeedConfigurableUpdater,
  repository: string,
  fetchImpl: typeof fetch = fetch,
): Promise<string> {
  const normalizedRepository = normalizeRepository(repository);
  const atomUrl = `https://github.com/${normalizedRepository}/releases.atom`;
  const response = await fetchImpl(atomUrl, {
    headers: { Accept: "application/atom+xml" },
  });
  if (!response.ok) {
    throw new Error(`GitHub Releases feed responded ${response.status}`);
  }

  const tag = latestDesktopReleaseTagFromAtom(await response.text());
  if (!tag) {
    throw new Error(
      `No ${DESKTOP_RELEASE_TAG_PREFIX}* Release found in ${normalizedRepository}`,
    );
  }

  const releaseBaseUrl =
    `https://github.com/${normalizedRepository}/releases/download/` +
    `${encodeURIComponent(tag)}/`;
  updater.setFeedURL({ provider: "generic", url: releaseBaseUrl });
  return tag;
}
