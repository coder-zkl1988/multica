import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { ProductStatusBadge, ProductNodeDetail } from "./index";
import type { ProductMapNode } from "@multica/core/products";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: undefined, isLoading: false }),
}));

const releasedNode: ProductMapNode = {
  id: "n1",
  workspace_id: "ws-1",
  name: "Multica",
  slug: "multica",
  description: "desc",
  sort_order: 1,
  status: "released",
  status_source: "code_repo",
  evidence: { source: "code_repo", repo_url: "https://gitlab.sy.soyoung.com/fe/wasai/multica.git" },
  created_at: "2026-08-11T00:00:00Z",
  updated_at: "2026-08-11T00:00:00Z",
  refs: [{ ref_type: "project", ref_id: "p1" }],
  editors: [{ user_id: "u1" }],
  has_live_evidence: true,
};

const pendingNode: ProductMapNode = {
  ...releasedNode,
  id: "n2",
  name: "院务系统",
  slug: "yuanwu",
  status: "pending_confirmation",
  status_source: "pmo",
  evidence: { source: "pmo", note: "no pmo data yet" },
  has_live_evidence: false,
};

describe("ProductStatusBadge", () => {
  it("labels released with evidence as Released", () => {
    renderWithI18n(<ProductStatusBadge node={releasedNode} />);
    expect(screen.getByText(/Released/)).toBeTruthy();
    expect(screen.getByText(/has evidence/)).toBeTruthy();
  });

  it("labels evidence-less node as Pending confirmation", () => {
    renderWithI18n(<ProductStatusBadge node={pendingNode} />);
    expect(screen.getByText(/Pending confirmation/)).toBeTruthy();
  });
});

describe("ProductNodeDetail", () => {
  it("shows live evidence for a released code_repo node", () => {
    renderWithI18n(<ProductNodeDetail node={releasedNode} />);
    expect(screen.getByText("Multica")).toBeTruthy();
    expect(screen.getByText(/Code repository/)).toBeTruthy();
    expect(screen.getByText(/gitlab.sy.soyoung.com/)).toBeTruthy();
  });

  it("shows pending confirmation and does not claim live for a node with no evidence", () => {
    renderWithI18n(<ProductNodeDetail node={pendingNode} />);
    expect(screen.getByText("院务系统")).toBeTruthy();
    expect(screen.getAllByText(/Pending confirmation/).length).toBeGreaterThan(0);
    expect(screen.getByText("no pmo data yet")).toBeTruthy();
    expect(screen.queryByText(/Released/)).toBeNull();
  });

  it("shows an empty-state placeholder when no node is selected", () => {
    renderWithI18n(<ProductNodeDetail node={null} />);
    expect(screen.getByText(/Select a product node on the left/)).toBeTruthy();
  });
});
