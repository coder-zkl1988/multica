/**
 * TestApiClient — lightweight API helper for E2E test data setup/teardown.
 *
 * Uses raw fetch so E2E tests have zero build-time coupling to the web app.
 */

import "./env";
import { createHmac, randomBytes } from "node:crypto";
import pg from "pg";

// `||` (not `??`) so an empty `NEXT_PUBLIC_API_URL=` in .env still falls
// back to localhost. dotenv sets unset-vs-empty both as "" — treating them
// the same matches user intent.
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const JWT_SECRET = process.env.JWT_SECRET || "multica-dev-secret-change-in-production";

function signInternalToken(userId: string, email: string, expiresAt: number) {
  const encode = (value: object) => Buffer.from(JSON.stringify(value)).toString("base64url");
  const header = encode({ alg: "HS256", typ: "JWT" });
  const claims = encode({
    sub: userId,
    email,
    auth_source: "sso",
    iat: Math.floor(Date.now() / 1000),
    exp: expiresAt,
  });
  const unsigned = `${header}.${claims}`;
  const signature = createHmac("sha256", JWT_SECRET).update(unsigned).digest("base64url");
  return `${unsigned}.${signature}`;
}

function csrfTokenFor(token: string) {
  const nonce = randomBytes(16);
  const signature = createHmac("sha256", token).update(nonce).digest("hex");
  return `${nonce.toString("hex")}.${signature}`;
}

interface TestWorkspace {
  id: string;
  name: string;
  slug: string;
}

export type TestIssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type TestIssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export interface TestTableIssueSeed {
  title: string;
  status?: TestIssueStatus;
  priority?: TestIssuePriority;
  parentIssueId?: string | null;
  position?: number;
}

export interface TestTableIssue {
  id: string;
  title: string;
  status: TestIssueStatus;
  number: number;
}

export class TestApiClient {
  private token: string | null = null;
  private csrfToken: string | null = null;
  private expiresAt: number | null = null;
  private workspaceSlug: string | null = null;
  private workspaceId: string | null = null;
  private email: string | null = null;
  private createdIssueIds: string[] = [];
  private seededIssueIds: string[] = [];

  async login(email: string, name: string) {
    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const normalizedEmail = email.trim().toLowerCase();
      const result = await client.query<{
        id: string;
        name: string;
        email: string;
        account_kind: string;
      }>(
        `INSERT INTO "user" AS existing_user (name, email, account_kind)
         VALUES ($1, $2, 'human')
         ON CONFLICT (email) DO UPDATE
           SET name = EXCLUDED.name, updated_at = now()
           WHERE existing_user.account_kind = 'human'
         RETURNING id, name, email, account_kind`,
        [name, normalizedEmail],
      );
      if (result.rows.length === 0) {
        throw new Error(`E2E login email belongs to a service account: ${normalizedEmail}`);
      }
      const user = result.rows[0];
      this.expiresAt = Math.floor(Date.now() / 1000) + 60 * 60;
      this.token = signInternalToken(user.id, user.email, this.expiresAt);
      this.csrfToken = csrfTokenFor(this.token);
      this.email = user.email;
      return { token: this.token, user };
    } finally {
      await client.end();
    }
  }

  async getWorkspaces(): Promise<TestWorkspace[]> {
    const res = await this.authedFetch("/api/workspaces");
    return res.json();
  }

  setWorkspaceId(id: string) {
    this.workspaceId = id;
  }

  setWorkspaceSlug(slug: string) {
    this.workspaceSlug = slug;
  }

  async ensureWorkspace(name = "E2E Workspace", slug = "e2e-workspace") {
    const workspaces = await this.getWorkspaces();
    let workspace = workspaces.find((item) => item.slug === slug) ?? workspaces[0];
    if (!workspace) {
      const res = await this.authedFetch("/api/workspaces", {
        method: "POST",
        body: JSON.stringify({ name, slug }),
      });
      if (res.ok) {
        workspace = (await res.json()) as TestWorkspace;
      } else {
        const refreshed = await this.getWorkspaces();
        workspace = refreshed.find((item) => item.slug === slug) ?? refreshed[0];
      }
    }

    if (!workspace) {
      throw new Error(`Failed to ensure workspace ${slug}`);
    }

    this.workspaceId = workspace.id;
    this.workspaceSlug = workspace.slug;
    const questionnaire = await this.authedFetch("/api/me/onboarding", {
      method: "PATCH",
      body: JSON.stringify({
        questionnaire: {
          source: [],
          source_other: "",
          source_skipped: true,
          role: "",
          role_other: "",
          role_skipped: true,
          use_case: [],
          use_case_other: "",
          use_case_skipped: true,
          version: 2,
        },
      }),
    });
    if (!questionnaire.ok) {
      throw new Error(`Failed to complete E2E questionnaire: ${questionnaire.status}`);
    }
    const onboarding = await this.authedFetch("/api/me/onboarding/complete", {
      method: "POST",
      body: JSON.stringify({ completion_path: "skip_existing", workspace_id: workspace.id }),
    });
    if (!onboarding.ok) {
      throw new Error(`Failed to complete E2E onboarding: ${onboarding.status}`);
    }
    return workspace;
  }

  async markUserOnboarded() {
    if (!this.email) {
      throw new Error("Cannot mark E2E user onboarded before login");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      const result = await client.query(
        `
          UPDATE "user"
          SET
            onboarded_at = COALESCE(onboarded_at, now()),
            onboarding_questionnaire = COALESCE(onboarding_questionnaire, '{}'::jsonb)
              || '{"source":["friends_colleagues"],"source_other":null,"source_skipped":false}'::jsonb
          WHERE email = $1
        `,
        [this.email],
      );
      if (result.rowCount !== 1) {
        throw new Error(`Failed to mark E2E user onboarded: ${this.email}`);
      }
    } finally {
      await client.end();
    }
  }

  async createIssue(title: string, opts?: Record<string, unknown>) {
    const res = await this.authedFetch("/api/issues", {
      method: "POST",
      body: JSON.stringify({ title, ...opts }),
    });
    const issue = await res.json();
    this.createdIssueIds.push(issue.id);
    return issue;
  }

  /**
   * Insert a large, deterministic issue fixture in one transaction.
   *
   * Browser E2E coverage for cursor-backed Table views needs 1,000+ rows,
   * which would make setup itself dominate the test if every row went through
   * the HTTP create endpoint. These rows intentionally contain no dependent
   * records; cleanup deletes exactly the returned IDs from the isolated E2E
   * workspace.
   */
  async seedTableIssues(rows: TestTableIssueSeed[]): Promise<TestTableIssue[]> {
    if (rows.length === 0) return [];
    if (!this.workspaceId || !this.email) {
      throw new Error("Cannot seed table issues before login and workspace setup");
    }

    const client = new pg.Client(DATABASE_URL);
    await client.connect();
    try {
      await client.query("BEGIN");
      const userResult = await client.query<{ id: string }>(
        `SELECT id FROM "user" WHERE email = $1`,
        [this.email],
      );
      const creatorId = userResult.rows[0]?.id;
      if (!creatorId) {
        throw new Error(`Cannot resolve E2E creator for ${this.email}`);
      }

      const counterResult = await client.query<{ issue_counter: number }>(
        `
          UPDATE workspace
          SET issue_counter = issue_counter + $2
          WHERE id = $1
          RETURNING issue_counter
        `,
        [this.workspaceId, rows.length],
      );
      const finalCounter = Number(counterResult.rows[0]?.issue_counter);
      if (!Number.isFinite(finalCounter)) {
        throw new Error(`Cannot reserve issue numbers for workspace ${this.workspaceId}`);
      }
      const firstNumber = finalCounter - rows.length + 1;

      const inserted = await client.query<TestTableIssue>(
        `
          INSERT INTO issue (
            workspace_id,
            title,
            status,
            priority,
            creator_type,
            creator_id,
            parent_issue_id,
            position,
            number
          )
          SELECT
            $1::uuid,
            fixture.title,
            fixture.status,
            fixture.priority,
            'member',
            $2::uuid,
            fixture.parent_issue_id,
            fixture.position,
            fixture.number
          FROM unnest(
            $3::text[],
            $4::text[],
            $5::text[],
            $6::uuid[],
            $7::double precision[],
            $8::integer[]
          ) WITH ORDINALITY AS fixture(
            title,
            status,
            priority,
            parent_issue_id,
            position,
            number,
            ordinal
          )
          ORDER BY fixture.ordinal
          RETURNING id, title, status, number
        `,
        [
          this.workspaceId,
          creatorId,
          rows.map((row) => row.title),
          rows.map((row) => row.status ?? "backlog"),
          rows.map((row) => row.priority ?? "none"),
          rows.map((row) => row.parentIssueId ?? null),
          rows.map((row, index) => row.position ?? index + 1),
          rows.map((_row, index) => firstNumber + index),
        ],
      );
      await client.query("COMMIT");
      this.seededIssueIds.push(...inserted.rows.map((row) => row.id));
      return inserted.rows;
    } catch (error) {
      await client.query("ROLLBACK");
      throw error;
    } finally {
      await client.end();
    }
  }

  async deleteIssue(id: string) {
    await this.authedFetch(`/api/issues/${id}`, { method: "DELETE" });
  }

  async updateIssue(id: string, updates: Record<string, unknown>) {
    const res = await this.authedFetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(updates),
    });
    if (!res.ok) {
      throw new Error(`update issue failed: ${res.status} ${await res.text()}`);
    }
    return res.json();
  }

  /** Clean up all issues created during this test. */
  async cleanup() {
    if (this.seededIssueIds.length > 0 && this.workspaceId) {
      const client = new pg.Client(DATABASE_URL);
      await client.connect();
      try {
        await client.query(
          `DELETE FROM issue WHERE workspace_id = $1 AND id = ANY($2::uuid[])`,
          [this.workspaceId, this.seededIssueIds],
        );
      } finally {
        await client.end();
      }
      this.seededIssueIds = [];
    }
    for (const id of this.createdIssueIds) {
      try {
        await this.deleteIssue(id);
      } catch {
        /* ignore — may already be deleted */
      }
    }
    this.createdIssueIds = [];
  }

  getToken() {
    return this.token;
  }

  getEmail() {
    if (!this.email) {
      throw new Error("Test API client is not logged in");
    }
    return this.email;
  }

  getBrowserSession() {
    if (!this.token || !this.csrfToken || !this.expiresAt) {
      throw new Error("test api client not logged in");
    }
    return { token: this.token, csrfToken: this.csrfToken, expiresAt: this.expiresAt };
  }

  private async authedFetch(path: string, init?: RequestInit) {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...((init?.headers as Record<string, string>) ?? {}),
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    if (this.workspaceSlug) headers["X-Workspace-Slug"] = this.workspaceSlug;
    else if (this.workspaceId) headers["X-Workspace-ID"] = this.workspaceId;
    return fetch(`${API_BASE}${path}`, { ...init, headers });
  }
}
