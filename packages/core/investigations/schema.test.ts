import { describe, expect, it } from "vitest";
import { InvestigationDetailSchema, InvestigationStatisticsSchema } from "./schema";

describe("investigation schemas", () => {
  it("defaults timeline collections for older detail responses", () => {
    const parsed = InvestigationDetailSchema.parse({
      id: "investigation-1",
      workspace_id: "workspace-1",
      title: "Timeout",
      description: "Checkout timed out",
      environment: "production",
      agent_id: "agent-1",
      status: "investigating",
      project_id: null,
      created_by: "user-1",
      created_at: "2026-08-24T00:00:00Z",
      updated_at: "2026-08-24T00:00:00Z",
    });

    expect(parsed).toMatchObject({ comments: [], tasks: [], attachments: [], evidence: [] });
  });

  it("rejects nullable numeric statistics", () => {
    expect(InvestigationStatisticsSchema.safeParse({ diagnosis_average: null }).success).toBe(false);
  });
});
