import { Download } from "lucide-react";
import { buttonVariants } from "@multica/ui/components/ui/button";

const DEFAULT_FIGMA_PLUGIN_DOWNLOAD_URL = "https://static.soyoung.com/sy-pre/multica-figma-plugin-1786608600611.zip";

export function FigmaPluginDownload({ downloadUrl }: { downloadUrl?: string }) {
  const url = downloadUrl?.trim() || DEFAULT_FIGMA_PLUGIN_DOWNLOAD_URL;

  return (
    <a
      href={url}
      download="multica-figma-plugin.zip"
      aria-label="下载 Figma 插件"
      className={buttonVariants({ variant: "outline", size: "sm" })}
    >
      <Download className="h-3.5 w-3.5" />
      <span className="hidden sm:inline">下载 Figma 插件</span>
      <span className="sm:hidden">下载</span>
    </a>
  );
}
