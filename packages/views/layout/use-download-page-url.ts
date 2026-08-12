"use client";

/**
 * URL of the marketing /download page, shared by every dashboard entry
 * point that links there (sidebar row, help menu).
 *
 * Fixed to the iworker download portal instead of the web origin: on
 * desktop the renderer is not the web origin, so a hard-coded absolute URL
 * is what makes the link land in the system browser correctly.
 */
export function useDownloadPageUrl(): string {
  return "https://iworker.soyoung.com/download";
}
