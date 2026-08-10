import {
  parseReleaseAssets,
  type DownloadAssets,
} from "./parse-release-assets";

/**
 * Server-side fetcher for the latest Multica release, designed to
 * run inside a Next.js server component. Response is cached by the
 * Next.js fetch cache for 5 minutes (Vercel ISR) so hitting /download
 * costs at most one GitHub API call per region per 5 minutes.
 *
 * This SSO distribution intentionally reads the fork's signed desktop
 * releases so an upstream release cannot silently replace it. CLI-only tags,
 * drafts, and other releases are ignored.
 *
 * On any failure (network, rate limit, malformed payload) returns a
 * `null`-shaped result and logs — the page degrades to a "version
 * unavailable" view rather than 500ing.
 */

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
}

const GITHUB_RELEASE_URL =
  "https://api.github.com/repos/coder-zkl1988/multica/releases?per_page=20";

const REVALIDATE_SECONDS = 300;

interface GitHubReleasePayload {
  tag_name?: string;
  published_at?: string;
  html_url?: string;
  prerelease?: boolean;
  draft?: boolean;
  assets?: Array<{ name: string; browser_download_url: string }>;
}

export async function fetchLatestRelease(): Promise<LatestRelease> {
  const headers: Record<string, string> = {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
  };
  // Optional PAT for local development and self-hosted deploys where
  // the shared outbound IP keeps hitting the 60-requests/hour
  // unauthenticated limit. Vercel's fetch cache is shared across all
  // regions so production rarely needs this — but the env var lets
  // anyone running the site locally avoid the rate-limit dance. Never
  // prefix this with `NEXT_PUBLIC_`; the token must stay server-side.
  const token = process.env.GITHUB_TOKEN;
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  try {
    const res = await fetch(GITHUB_RELEASE_URL, {
      next: { revalidate: REVALIDATE_SECONDS },
      headers,
    });
    if (!res.ok) {
      throw new Error(`GitHub API responded ${res.status}`);
    }
    const releases = (await res.json()) as GitHubReleasePayload[];
    const chosen = releases.find(
      (release) =>
        !release.draft && release.tag_name?.startsWith("desktop-v"),
    );
    if (!chosen) return emptyRelease();

    return {
      version: chosen.tag_name ?? null,
      publishedAt: chosen.published_at ?? null,
      htmlUrl: chosen.html_url ?? null,
      assets: parseReleaseAssets(chosen.assets ?? []),
    };
  } catch (err) {
    console.warn("[download] fetchLatestRelease failed:", err);
    return emptyRelease();
  }
}

function emptyRelease(): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    assets: {},
  };
}
