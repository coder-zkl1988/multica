import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ProjectDesignSystemLocator,
  ProjectDesignSystemPreviewVerificationReceipt,
  ProjectDesignSystemScope,
} from "@multica/core/types";
import { ProjectDesignSystemPreview } from "./project-design-system-preview";

const locators: ProjectDesignSystemLocator[] = [
  { id: "overview", kind: "block", label: "Overview" },
  { id: "button-primary", kind: "component", label: "Primary button" },
];

afterEach(() => vi.useRealTimers());

function dispatchSelection(id: string, source: MessageEventSource | null, capability?: string) {
	const event = new MessageEvent("message", {
    data: { type: "multica:project-design-system-select", id, capability },
  });
  Object.defineProperty(event, "source", { value: source });
  window.dispatchEvent(event);
}

function dispatchVerification(
  receipt: Partial<ProjectDesignSystemPreviewVerificationReceipt>,
  source: MessageEventSource | null,
) {
  const event = new MessageEvent("message", {
    data: {
      type: "multica:project-design-system-preview",
      status: "ready",
      digest: "digest-1",
      reason: "",
      locator_count: 2,
      visible_locator_count: 1,
      body_width: 1280,
      body_height: 720,
      image_count: 0,
      failed_image_count: 0,
      ...receipt,
    },
  });
  Object.defineProperty(event, "source", { value: source });
  window.dispatchEvent(event);
}

describe("ProjectDesignSystemPreview", () => {
  it("loads verified archive targets by URL and switches between UI Kit and Preview", async () => {
    const user = userEvent.setup();
    render(
      <ProjectDesignSystemPreview
        previewHtml="legacy preview"
        archiveTargets={[
          {
            kind: "preview",
            id: "colors-palette",
            path: "preview/colors-palette.html",
            url: "/api/preview/colors-palette.html",
          },
          {
            kind: "ui_kit",
            id: "app",
            path: "ui_kits/app/index.html",
            url: "/api/preview/ui_kits/app/index.html",
          },
        ]}
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={vi.fn()}
      />,
    );

    const frame = screen.getByTitle("项目设计体系 UI Kit");
    expect(frame).toHaveAttribute("src", "/api/preview/ui_kits/app/index.html");
    expect(frame).not.toHaveAttribute("srcdoc");
    expect(screen.getByRole("combobox", { name: "预览内容" })).toHaveValue("ui_kit:app");

    await user.selectOptions(screen.getByRole("combobox", { name: "预览内容" }), "preview:colors-palette");
    expect(screen.getByTitle("项目设计体系 UI Kit")).toHaveAttribute("src", "/api/preview/colors-palette.html");
  });

  it("centers a mobile viewport and switches size modes without reloading the iframe", async () => {
    const user = userEvent.setup();
    render(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><main>Mobile UI Kit</main></body></html>"
        platform="mobile"
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={vi.fn()}
      />,
    );

    const frame = screen.getByTitle("项目设计体系 UI Kit");
    const fitButton = screen.getByRole("button", { name: "适应画布" });
    const actualButton = screen.getByRole("button", { name: "原始尺寸" });
    const canvas = screen.getByRole("region", { name: "UI Kit 预览画布" });
    expect(screen.getByText("移动端 · 390 px")).toBeInTheDocument();
    expect(canvas).toHaveClass("justify-center");
    expect(frame.parentElement).toHaveClass("w-full", "max-w-[390px]");
    expect(fitButton).toHaveAttribute("aria-pressed", "true");
    expect(actualButton).toHaveAttribute("aria-pressed", "false");

    await user.click(actualButton);

    expect(actualButton).toHaveAttribute("aria-pressed", "true");
    expect(frame.parentElement).toHaveStyle({ width: "390px" });
    expect(canvas).toHaveClass("justify-center");
    expect(screen.getByTitle("项目设计体系 UI Kit")).toBe(frame);
  });

  it("uses a 1280px original viewport for web design systems", async () => {
    const user = userEvent.setup();
    render(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><main>Web UI Kit</main></body></html>"
        platform="web"
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={vi.fn()}
      />,
    );

    expect(screen.getByText("Web · 1280 px")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "原始尺寸" }));
    expect(screen.getByTitle("项目设计体系 UI Kit").parentElement).toHaveStyle({ width: "1280px" });
  });

  it("uses sandbox allow-scripts without same-origin forms navigation or popups", () => {
    render(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><button>Save</button></body></html>"
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={vi.fn()}
      />,
    );

    const frame = screen.getByTitle("项目设计体系 UI Kit");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    const sandbox = frame.getAttribute("sandbox") ?? "";
    expect(sandbox).not.toContain("allow-same-origin");
    expect(sandbox).not.toContain("allow-forms");
    expect(sandbox).not.toContain("allow-top-navigation");
    expect(sandbox).not.toContain("allow-popups");
  });

  it("keeps locator selection disabled in the default browse mode", () => {
    const onSelect = vi.fn<(scope: ProjectDesignSystemScope) => void>();
    render(
      <ProjectDesignSystemPreview
        previewHtml=""
        archiveTargets={[{
          kind: "ui_kit",
          id: "app",
          path: "ui_kits/app/index.html",
          url: "/api/project-design-system-previews/workspace/system/digest/resource-capability/files/ui_kits/app/index.html",
        }]}
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={onSelect}
      />,
    );
    const frame = screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement;
    dispatchSelection("button-primary", frame.contentWindow, "resource-capability");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("accepts locator messages only from its own iframe and known IDs", () => {
    const onSelect = vi.fn<(scope: ProjectDesignSystemScope) => void>();
    render(
      <ProjectDesignSystemPreview
        previewHtml=""
        archiveTargets={[{
          kind: "ui_kit",
          id: "app",
          path: "ui_kits/app/index.html",
          url: "/api/project-design-system-previews/workspace/system/digest/resource-capability/files/ui_kits/app/index.html",
        }]}
        locators={locators}
        integritySha256="digest-1"
        selectionActive
        onVerification={vi.fn()}
        onSelect={onSelect}
      />,
    );

    const frame = screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement;
    dispatchSelection("button-primary", window, "resource-capability");
    dispatchSelection("unknown", frame.contentWindow, "resource-capability");
    dispatchSelection("button-primary", frame.contentWindow);
    dispatchSelection("button-primary", frame.contentWindow, "wrong-capability");
    expect(onSelect).not.toHaveBeenCalled();

    dispatchSelection("button-primary", frame.contentWindow, "resource-capability");
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith({ kind: "component", id: "button-primary" });
  });

  it("uses the native archive selection bridge without submitting browser verification", () => {
    const onVerification = vi.fn();
    const onSelect = vi.fn<(scope: ProjectDesignSystemScope) => void>();
    render(
      <ProjectDesignSystemPreview
        previewHtml=""
        archiveTargets={[{ kind: "ui_kit", id: "app", path: "ui_kits/app/index.html", url: "/api/project-design-system-previews/workspace/system/digest/resource-capability/files/ui_kits/app/index.html" }]}
        locators={locators}
        integritySha256="digest-1"
        packageSchema="multica.project-design-system/v2"
        selectionActive
        onVerification={onVerification}
        onSelect={onSelect}
      />,
    );
    const frame = screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement;
    dispatchSelection("button-primary", frame.contentWindow, "resource-capability");
    dispatchVerification({}, frame.contentWindow);
    expect(onSelect).toHaveBeenCalledWith({ kind: "component", id: "button-primary" });
    expect(onVerification).not.toHaveBeenCalled();
  });

  it("accepts one matching verification receipt from its own iframe", () => {
    const onVerification = vi.fn<(receipt: ProjectDesignSystemPreviewVerificationReceipt) => void>();
    render(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><main>CRM</main></body></html>"
        locators={locators}
        integritySha256="digest-1"
        onVerification={onVerification}
        onSelect={vi.fn()}
      />,
    );

    const frame = screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement;
    dispatchVerification({}, window);
    dispatchVerification({ digest: "other-digest" }, frame.contentWindow);
    dispatchVerification({ visible_locator_count: 3 }, frame.contentWindow);
    expect(onVerification).not.toHaveBeenCalled();

    dispatchVerification({}, frame.contentWindow);
    dispatchVerification({}, frame.contentWindow);
    expect(onVerification).toHaveBeenCalledTimes(1);
    expect(onVerification).toHaveBeenCalledWith({
      status: "ready",
      digest: "digest-1",
      reason: "",
      locator_count: 2,
      visible_locator_count: 1,
      body_width: 1280,
      body_height: 720,
      image_count: 0,
      failed_image_count: 0,
    });
  });

  it("reports a bounded failure on timeout and permits an explicit retry attempt", async () => {
    vi.useFakeTimers();
    const onVerification = vi.fn<(receipt: ProjectDesignSystemPreviewVerificationReceipt) => void>();
    const { rerender } = render(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><main>CRM</main></body></html>"
        locators={locators}
        integritySha256="digest-1"
        verificationAttempt={0}
        onVerification={onVerification}
        onSelect={vi.fn()}
      />,
    );

    await vi.advanceTimersByTimeAsync(8_000);
    expect(onVerification).toHaveBeenCalledTimes(1);
    expect(onVerification).toHaveBeenLastCalledWith(expect.objectContaining({
      status: "failed",
      digest: "digest-1",
      reason: "measurement_failed",
    }));

    rerender(
      <ProjectDesignSystemPreview
        previewHtml="<!doctype html><html><body><main>CRM</main></body></html>"
        locators={locators}
        integritySha256="digest-1"
        verificationAttempt={1}
        onVerification={onVerification}
        onSelect={vi.fn()}
      />,
    );
    const frame = screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement;
    dispatchVerification({}, frame.contentWindow);
    expect(onVerification).toHaveBeenCalledTimes(2);
    expect(onVerification).toHaveBeenLastCalledWith(expect.objectContaining({ status: "ready" }));
    vi.useRealTimers();
  });

  it("shows a preview error instead of treating blank HTML as success", () => {
    render(
      <ProjectDesignSystemPreview
        previewHtml="   "
        locators={locators}
        integritySha256="digest-1"
        onVerification={vi.fn()}
        onSelect={vi.fn()}
      />,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("UI Kit 暂时不可用");
    expect(screen.queryByTitle("项目设计体系 UI Kit")).not.toBeInTheDocument();
  });
});
