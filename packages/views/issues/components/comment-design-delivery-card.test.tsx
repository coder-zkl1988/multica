// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { act, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { renderWithI18n } from "../../test/i18n";
import { CommentDesignDeliveryCard, inlineDeliveryPreview } from "./comment-design-delivery-card";
import type { AgentTask, TaskMessagePayload } from "@multica/core/types";

vi.mock("@multica/core/hooks", async (original) => ({ ...await original<typeof import("@multica/core/hooks")>(), useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/paths", async (original) => ({ ...await original<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => ({ designDocumentDetail: (id: string) => id }) }));
vi.mock("../../navigation", async (original) => ({ ...await original<typeof import("../../navigation")>(), useNavigation: () => ({ push: vi.fn() }) }));

describe("live delivery preview isolation", () => {
  it("renders real package content without executable code or navigation", async () => {
    const snapshot = {
      task_id: "task-1", document_id: "document-1", content_digest: "digest-1", updated_at: "2026-09-08",
      entry_path: "prototype/index.html",
      files: {
        "prototype/index.html": btoa('<html><head><meta http-equiv="refresh" content="0;url=https://outside.example"><link rel="stylesheet" href="screen.css"></head><body><h1>Checkout draft</h1><script>window.location="https://outside.example"</script><a href="https://outside.example">Pay</a><iframe src="https://outside.example"></iframe></body></html>'),
        "prototype/screen.css": btoa("h1 { color: rebeccapurple; }"),
      },
    };
    const result = await inlineDeliveryPreview(snapshot);
    const document = new DOMParser().parseFromString(result.html, "text/html");
    expect(document.querySelector("h1")?.textContent).toBe("Checkout draft");
    expect(document.querySelector("style")?.textContent).toContain("rebeccapurple");
    expect(document.querySelector("script, iframe, meta[http-equiv=refresh], a[href]")).toBeNull();
    expect(document.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute("content")).toContain("default-src 'none'");
    expect(result.missing).toEqual([]);
  });
  it("does not manufacture a preview when the actual entry is missing", async () => {
    await expect(inlineDeliveryPreview({ task_id: "task-1", document_id: "document-1", content_digest: "digest-1", updated_at: "2026-09-08", entry_path: "prototype/index.html", files: {} })).rejects.toThrow();
  });
});

it("keeps reported progress isolated by task and preserves incomplete progress on failure", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, refetchOnMount: false } } });
  const first = "11111111-1111-4111-8111-111111111111";
  const second = "22222222-2222-4222-8222-222222222222";
  const taskKey = issueTasksOptions("workspace", "issue").queryKey;
  const task = (id: string, status: AgentTask["status"] = "running", error: string | null = null): AgentTask => ({ id, status, error, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", priority: 0, dispatched_at: null, started_at: null, completed_at: null, result: null, created_at: "2026-09-08T00:00:00Z" });
  client.setQueryData(taskKey, [task(first), task(second)]);
  const plan = (task_id: string, stage: string, seq: number): TaskMessagePayload => ({ task_id, issue_id: "issue", seq, type: "tool_use", tool: "update_plan", input: { plan: [{ step: "Read source", status: "completed" }, { step: stage, status: "in_progress" }] } });
  client.setQueryData(taskMessagesOptions(first).queryKey, [plan(first, "Build checkout", 1), { task_id: first, issue_id: "issue", seq: 2, type: "thinking", content: "Private thought must not appear" }]);
  client.setQueryData(taskMessagesOptions(second).queryKey, [plan(second, "Build settings", 1)]);
  const view = renderWithI18n(<QueryClientProvider client={client}>
    <CommentDesignDeliveryCard issueId="issue" delivery={{ operation: "design", task_id: first, agent_id: "agent", project_resource_id: "repository" }} />
    <CommentDesignDeliveryCard issueId="issue" delivery={{ operation: "design", task_id: second, agent_id: "agent", project_resource_id: "repository" }} />
  </QueryClientProvider>);
  const firstCard = view.container.querySelector('[data-design-delivery-task="' + first + '"]') as HTMLElement;
  const secondCard = view.container.querySelector('[data-design-delivery-task="' + second + '"]') as HTMLElement;
  expect(firstCard).toHaveTextContent("Build checkout");
  expect(firstCard).not.toHaveTextContent("Build settings");
  expect(secondCard).toHaveTextContent("Build settings");
  expect(view.container).not.toHaveTextContent("Private thought must not appear");
  expect(firstCard).toHaveTextContent("1/2");
  act(() => {
    client.setQueryData(taskMessagesOptions(first).queryKey, [plan(first, "Verify checkout", 3)]);
    client.setQueryData(taskKey, [task(first, "failed", "Preview rejected"), task(second)]);
  });
  await waitFor(() => expect(firstCard).toHaveTextContent("Verify checkout"));
  expect(firstCard).toHaveTextContent("Preview rejected");
  expect(firstCard).toHaveTextContent("1/2");
  expect(firstCard).not.toHaveTextContent("2/2");
  expect(secondCard).toHaveTextContent("Build settings");
  expect(within(firstCard).queryByText("Build checkout")).not.toBeInTheDocument();
  view.unmount();
  client.clear();
});
