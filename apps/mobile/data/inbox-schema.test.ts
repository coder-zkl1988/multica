import { describe, expect, it } from "vitest";
import { InboxListSchema } from "./schemas";

/**
 * Tests for mobile's CLIENT-SIDE parsing of GET /api/inbox.
 *
 * Scope, stated precisely because the name of this file used to overclaim:
 * these are hand-written fixtures run against `InboxListSchema`. They pin how
 * this client REACTS to a given payload. They cannot fail when the Go server
 * starts sending something new — nothing here executes server code.
 *
 * The server stores `details` as JSON and some notification producers include
 * numeric metrics (for example, autopilot failure counts). Mobile normalizes
 * scalar values to strings because the shared `InboxItem` type and row labels
 * consume string detail values.
 *
 * Why this boundary matters: because the endpoint parses an ARRAY, one schema
 * failure would make `listInbox` fall back to `EMPTY_INBOX_LIST`, blanking the
 * entire mobile inbox instead of only one row. The coercion keeps valid rows
 * visible while malformed detail shapes remain rejected.
 */
describe("inbox list schema", () => {
  it("parses a row shaped like the documented server payload", () => {
    const serverRow = {
      id: "inbox-1",
      workspace_id: "ws-1",
      recipient_type: "member",
      recipient_id: "user-1",
      type: "status_changed",
      severity: "info",
      issue_id: "issue-1",
      title: "P0: delegated subscription rule",
      body: "",
      actor_type: "agent",
      actor_id: "agent-1",
      read: false,
      archived: false,
      created_at: "2026-07-30T00:00:00Z",
      // Numeric server metrics are coerced to strings at the mobile boundary.
      details: { from: "in_progress", to: "in_review", failed_runs: 3 },
    };

    const parsed = InboxListSchema.safeParse([serverRow]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.type).toBe("status_changed");
    expect(parsed.success && parsed.data[0]?.details?.to).toBe("in_review");
  });

  it("coerces numeric details values", () => {
    const serverRow = {
      id: "inbox-2",
      recipient_type: "member",
      type: "autopilot_paused",
      details: { failed_runs: 3, fail_pct: 75.5 },
    };

    const parsed = InboxListSchema.safeParse([serverRow]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.details).toEqual({
      failed_runs: "3",
      fail_pct: "75.5",
    });
  });

  it("rejects a malformed details shape", () => {
    const badRow = {
      id: "inbox-3",
      recipient_type: "member",
      type: "status_changed",
      details: "not-an-object",
    };

    expect(InboxListSchema.safeParse([badRow]).success).toBe(false);
  });
  it("keeps a malformed row from emptying the entire list observable", () => {
    // The schema is an array, so a malformed detail shape invalidates every
    // row and listInbox falls back to EMPTY_INBOX_LIST.
    const good = {
      id: "inbox-4",
      recipient_type: "member",
      type: "status_changed",
      details: { from: "todo", to: "in_review" },
    };
    const bad = {
      id: "inbox-5",
      recipient_type: "member",
      type: "status_changed",
      details: "not-an-object",
    };

    expect(InboxListSchema.safeParse([good]).success).toBe(true);
    expect(InboxListSchema.safeParse([good, bad]).success).toBe(false);
  });


  it("renders an unknown server type instead of dropping the row", () => {
    // Mirrors the root CLAUDE.md API-compatibility rule and mobile's own
    // "render every inbox type, never silently drop a category" parity rule: a
    // type this build has never heard of must still parse.
    const future = {
      id: "inbox-5",
      recipient_type: "member",
      type: "some_future_type",
      details: { anything: "still a string" },
    };

    const parsed = InboxListSchema.safeParse([future]);
    expect(parsed.success).toBe(true);
  });
});
