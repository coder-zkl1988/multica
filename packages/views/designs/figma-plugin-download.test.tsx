import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { FigmaPluginDownload } from "./figma-plugin-download";

const DOWNLOAD_URL = "https://static.soyoung.com/sy-design/releases/multica-figma-plugin.zip";
const DEFAULT_DOWNLOAD_URL = "https://static.soyoung.com/sy-pre/multica-figma-plugin-1786608600611.zip";

describe("FigmaPluginDownload", () => {
  it("renders a downloadable CDN link", () => {
    render(<FigmaPluginDownload downloadUrl={DOWNLOAD_URL} />);

    expect(screen.getByRole("link", { name: "下载 Figma 插件" })).toHaveAttribute("href", DOWNLOAD_URL);
    expect(screen.getByRole("link", { name: "下载 Figma 插件" })).toHaveAttribute("download", "multica-figma-plugin.zip");
  });

  it("uses the published CDN package when no override is configured", () => {
    render(<FigmaPluginDownload />);

    expect(screen.getByRole("link", { name: "下载 Figma 插件" })).toHaveAttribute("href", DEFAULT_DOWNLOAD_URL);
  });
});
