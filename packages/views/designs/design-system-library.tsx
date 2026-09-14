"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Boxes,
  ExternalLink,
  FolderOpen,
  GitBranch,
  Package,
  Plus,
  Search,
  SwatchBook,
} from "lucide-react";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import {
  builtinDesignSystemDetailOptions,
  builtinDesignSystemListOptions,
  projectDesignSystemCatalogueOptions,
  projectDesignSystemDetailOptions,
} from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  BuiltinDesignSystem,
  BuiltinDesignSystemArtifact,
  BuiltinDesignSystemDetail,
  ProjectDesignSystemCatalogueEntry,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { builtinDesignSystemLogoURL } from "./design-system-domains";
import { DesignFilterPill } from "./design-filter-pill";
import { DesignFilterSelect } from "./design-filter-select";
import { PLATFORM_OPTIONS } from "./design-task-composer";
import { ProjectDesignSystemPreview } from "./project-design-system-preview";

/**
 * Ownership scope. Saved project and repository systems remain one canonical
 * record; the catalogue projects them into 我的 when the current requester
 * created them, or 团队 when another workspace member did.
 */
type LibraryScope = "mine" | "team" | "official";

const SCOPE_LABELS: ReadonlyArray<{ value: LibraryScope; label: string }> = [
  { value: "mine", label: "我的" },
  { value: "team", label: "团队" },
  { value: "official", label: "官方" },
];

const SCOPE_EMPTY_COPY: Record<LibraryScope, string> = {
  mine: "这里还没有你保存的设计体系。在项目或仓库中保存后，会作为同一套体系出现在这里。",
  team: "其他工作区成员还没有保存可共享的设计体系。",
  official: "没有匹配的官方设计体系。",
};

// Sentinel for "no platform picked". Platform is a closed enum, but an entry
// can carry an empty platform, so the filter needs a value neither can take.
const ALL_PLATFORMS = "__all__";

function platformLabel(platform: string): string {
  return PLATFORM_OPTIONS.find((option) => option.value === platform)?.label ?? "未标注平台";
}

/**
 * Four swatches for a system that declares none, seeded from its name —
 * Open Design's `fallbackSwatches` ported verbatim. Every row then carries a
 * colour identity even before its palette exists, and the same name always
 * paints the same stripes.
 */
export function paletteFallbackSwatches(seed: string): string[] {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  const base = h % 360;
  return [
    `hsl(${base}, 24%, 94%)`,
    `hsl(${(base + 90) % 360}, 34%, 74%)`,
    `hsl(${(base + 180) % 360}, 42%, 34%)`,
    `hsl(${(base + 28) % 360}, 76%, 54%)`,
  ];
}

/**
 * The palette stripe every design-system row leads with when no image
 * resolves — Open Design's `SystemRowPaletteLogo`: the system's own first
 * swatches, or the seeded fallback, painted as equal vertical bands filling
 * the whole mark. Bare colour, no frame; a row never shows a generic glyph.
 */
function RowPaletteMark({ swatches, seed }: { swatches: string[]; seed: string }) {
  const colors = swatches.length > 0 ? swatches.slice(0, 4) : paletteFallbackSwatches(seed);
  return (
    <span aria-hidden="true" className="flex size-10 shrink-0 overflow-hidden rounded-md">
      {colors.map((color, index) => (
        <span key={`${color}-${index}`} className="min-w-0 flex-1" style={{ background: color }} />
      ))}
    </span>
  );
}

function scopeOf(entry: ProjectDesignSystemCatalogueEntry): LibraryScope {
  return entry.ownership_scope ?? "team";
}

function SystemDetail({
  entry,
  onOpenSystem,
}: {
  entry: ProjectDesignSystemCatalogueEntry;
  onOpenSystem: (entry: ProjectDesignSystemCatalogueEntry) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: system, isLoading, error } = useQuery(projectDesignSystemDetailOptions(wsId, entry.id));
  const archivePreview = useQuery({
    queryKey: [
      ...designKeys.projectDesignSystemPackagePreview(wsId, entry.id),
      system?.preview_validation.integrity_sha256 || "saved",
    ],
    queryFn: () => api.getProjectDesignSystemPackagePreview(entry.id),
    enabled: Boolean(system?.id && system.preview_validation.status === "passed"),
    retry: false,
  });
  const archiveTargets = useMemo(() => {
    const preview = archivePreview.data;
    if (!preview?.content_digest || !preview.targets.length) return [];
    try {
      return preview.targets.map((target) => ({
        ...target,
        url: api.getProjectDesignSystemPackagePreviewFileURL(
          entry.id,
          wsId,
          preview.content_digest,
          preview.resource_access_token,
          target.path,
        ),
      }));
    } catch {
      return [];
    }
  }, [archivePreview.data, entry.id, wsId]);

  const isDraft = system?.status === "draft" || system?.has_unsaved_changes === true;
  const scopedToRepository = Boolean(entry.workspace_repository_id || entry.project_resource_id);

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">
      <header className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1 basis-60">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h2 className="min-w-0 break-words text-title font-semibold">
              {entry.name.trim() || "未命名设计体系"}
            </h2>
            {isDraft ? <Badge variant="secondary" className="shrink-0 px-1.5 text-micro font-normal">草稿</Badge> : null}
            <Badge variant="outline" className="shrink-0 gap-1 px-1.5 text-micro font-normal">
              {scopedToRepository ? <GitBranch className="size-3" /> : <Package className="size-3" />}
              {scopedToRepository ? "仓库专属" : "项目通用"}
            </Badge>
          </div>
          <p className="mt-1 min-w-0 break-words text-body text-muted-foreground">
            {entry.project_id
              ? `项目绑定 · ${entry.project_title || "未知项目"} · ${platformLabel(entry.platform)}`
              : `独立设计体系 · ${platformLabel(entry.platform)}`}
          </p>
        </div>
        <Button type="button" size="sm" variant="outline" className="h-7 shrink-0" onClick={() => onOpenSystem(entry)}>
          {scopedToRepository ? <GitBranch className="size-3.5" /> : entry.project_id ? <FolderOpen className="size-3.5" /> : <ExternalLink className="size-3.5" />}
          {scopedToRepository ? "打开仓库" : entry.project_id ? "打开项目" : "打开体系"}
        </Button>
      </header>

      {isLoading ? (
        <Skeleton className="min-h-[520px] w-full flex-1 rounded-xl" />
      ) : error || !system ? (
        <div className="rounded-xl border border-dashed px-4 py-12 text-center text-body text-muted-foreground">无法加载这套设计体系的内容。</div>
      ) : (
        <section className="flex min-h-[520px] flex-1 flex-col overflow-hidden rounded-xl border bg-white" aria-label="已保存设计体系 UI Kit">
          <ProjectDesignSystemPreview
            previewHtml={system.content.preview_html}
            archiveTargets={archiveTargets}
            platform={system.platform}
            locators={system.content.locators}
            integritySha256={system.content.integrity_sha256}
            selectionEnabled={false}
            packageSchema={system.content.package_schema}
            onVerification={() => {}}
            onSelect={() => {}}
          />
        </section>
      )}
    </div>
  );
}

function SystemListItem({
  entry,
  selected,
  onSelect,
}: {
  entry: ProjectDesignSystemCatalogueEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      // The selected row is a solid surface with a primary ring — visible on
      // the washed page background where a grey accent washes out — and the
      // hover compound is spelled out so hovering it cannot demote it.
      className={cn(
        "flex w-full cursor-pointer items-center gap-2.5 rounded-lg p-2 text-left transition-colors",
        selected
          ? "bg-background font-medium text-foreground shadow-sm ring-1 ring-primary/40 hover:bg-background"
          : "text-foreground hover:bg-accent/50",
      )}
    >
      <RowPaletteMark swatches={[]} seed={entry.name.trim() || entry.id} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-body">{entry.name.trim() || "未命名设计体系"}</span>
        <span className="mt-0.5 truncate text-caption font-normal text-muted-foreground">
          {/* OD's chain: the system's own summary first, ownership context as
              the fallback — a row describes the system before its shelf. */}
          {entry.summary.trim()
            || (entry.project_id
              ? `${entry.project_title || "未知项目"}${entry.project_resource_id ? " · 仓库专属" : " · 项目通用"}`
              : "独立设计体系")}
        </span>
      </span>
      {/* OD's row-end status marker, user systems only: a dot that is green
          when what is saved is current and amber while a draft sits beside
          it. Carried by colour and title, which hover never touches. */}
      <span
        title={entry.has_draft_package ? "已保存，另有未保存的调整草稿" : "已保存"}
        aria-label={entry.has_draft_package ? "已保存，另有未保存的调整草稿" : "已保存"}
        className={cn("size-2 shrink-0 rounded-full", entry.has_draft_package ? "bg-amber-500" : "bg-emerald-500")}
      />
    </button>
  );
}

const ALL_CATEGORIES = "__all__";


/**
 * Open Design's isLightHex: anything that is not a six-digit hex counts as
 * light, so the hex printed on the swatch stays dark-on-light by default.
 */
export function isLightHex(hex: string): boolean {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return true;
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return r * 0.299 + g * 0.587 + b * 0.114 > 150;
}

/** The dark showcase lives beside the light one; the server composes both paths. */
function showcaseVariantURL(showcaseUrl: string, variant: "light" | "dark"): string {
  if (!showcaseUrl) return "";
  return `${api.getBaseUrl()}${showcaseUrl.replace(/\/(light|dark)$/, "")}/${variant}`;
}

/**
 * 设计系统 — Open Design's kit module: the package's token-driven
 * system/kit.html framed with a 浅色/深色 toggle and the file name as its
 * caption, and the token contract chips from system/tokens.default.json
 * below it. No scripts run in the frame (the document has none and its CSP
 * admits none).
 */
function BuiltinKitModule({ system }: { system: BuiltinDesignSystemDetail }) {
  const [variant, setVariant] = useState<"light" | "dark">("light");
  const src = showcaseVariantURL(system.showcase_url, variant);
  if (!src) return null;
  return (
    <section className="flex flex-col gap-2" aria-label="设计系统">
      <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">设计系统</h3>
      <div className="overflow-hidden rounded-lg border">
        <div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-2 py-1">
          <div className="flex items-center gap-0.5">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-pressed={variant === "light"}
              className={cn("h-6 px-2 text-caption", variant === "light" && "bg-accent text-foreground")}
              onClick={() => setVariant("light")}
            >
              浅色
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-pressed={variant === "dark"}
              className={cn("h-6 px-2 text-caption", variant === "dark" && "bg-accent text-foreground")}
              onClick={() => setVariant("dark")}
            >
              深色
            </Button>
          </div>
          <div className="flex items-center gap-1">
            <span className="font-mono text-micro text-muted-foreground">system/kit.html</span>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label="在新标签页中打开"
              title="在新标签页中打开"
              onClick={() => window.open(src, "_blank", "noopener,noreferrer")}
            >
              <ExternalLink className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <iframe
          key={src}
          title={`${system.name || system.slug} 设计系统`}
          src={src}
          sandbox=""
          referrerPolicy="no-referrer"
          className="h-[420px] w-full border-0 bg-background"
        />
      </div>
      {system.token_contract.some((token) => token.name === "colorPrimary") ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {system.token_contract.map((token) => (
            <span key={token.name} className="inline-flex max-w-72 items-center gap-1.5 rounded-md border bg-card px-1.5 py-1">
              {token.value.startsWith("#") ? (
                <span aria-hidden="true" className="size-3.5 shrink-0 rounded-sm border" style={{ background: token.value }} />
              ) : null}
              <span className="shrink-0 font-mono text-micro font-medium">{token.name}</span>
              <span className="truncate font-mono text-micro text-muted-foreground">{token.value}</span>
            </span>
          ))}
        </div>
      ) : null}
    </section>
  );
}

/**
 * 设计系统素材 — Open Design's asset grid: the six derived pages framed live
 * at card size with their fixed captions. Clicking opens the page itself.
 */
function BuiltinArtifactCard({ artifact, systemName }: { artifact: BuiltinDesignSystemArtifact; systemName: string }) {
  const src = `${api.getBaseUrl()}${artifact.url}`;
  return (
    <button
      type="button"
      className="group/artifact flex min-w-0 cursor-pointer flex-col overflow-hidden rounded-lg border bg-card text-left transition-colors hover:border-primary/40"
      aria-label={`打开 ${artifact.label}`}
      onClick={() => window.open(src, "_blank", "noopener,noreferrer")}
    >
      <span className="relative block aspect-[4/3] overflow-hidden border-b bg-muted/30">
        <span className="pointer-events-none absolute left-0 top-0 block h-[300%] w-[300%] origin-top-left scale-[0.3333]">
          <iframe
            src={src}
            title={`${systemName} ${artifact.label}`}
            aria-hidden="true"
            tabIndex={-1}
            loading="lazy"
            sandbox=""
            referrerPolicy="no-referrer"
            className="h-full w-full border-0 bg-background"
          />
        </span>
      </span>
      <span className="truncate px-2.5 py-2 text-caption text-muted-foreground">{artifact.label}</span>
    </button>
  );
}

/**
 * The 官方 detail, structured as Open Design's kit view renders a package:
 * 品牌标识 → Logo → 字体排版 → 调色板 → 图像与布局 → 设计系统 → 设计系统素材,
 * every module fed by the field the server parsed from the package's own
 * files rather than by re-showing DESIGN.md wholesale.
 */
function BuiltinSystemDetail({ slug }: { slug: string }) {
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(builtinDesignSystemDetailOptions(wsId, slug));

  if (isLoading) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-24 w-full rounded-lg" />
        <Skeleton className="h-40 w-full rounded-lg" />
      </div>
    );
  }
  if (!data) return null;

  const sampleText = data.title || data.name || slug;
  const fonts = [
    { role: "Display", family: data.typography.display, sample: sampleText },
    { role: "Body", family: data.typography.body, sample: sampleText },
    { role: "Mono", family: data.typography.mono, sample: "const brand = await extract(url);" },
  ].filter((font) => font.family);
  const weights = data.typography.weights.join("/");

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-title font-medium">{data.title || data.name || slug}</h2>
          {data.category ? <Badge variant="secondary">{data.category}</Badge> : null}
          <Badge variant="outline">官方</Badge>
        </div>
        {data.description ? <p className="text-body text-muted-foreground">{data.description}</p> : null}
      </div>

      {data.identity ? (
        <section className="flex flex-col gap-2" aria-label="品牌标识">
          <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">品牌标识</h3>
          <p className="text-body leading-7 text-muted-foreground">{data.identity}</p>
        </section>
      ) : null}

      {/* Bundled packages ship no logo asset, exactly as Open Design's kit
          view shows for them: an empty slot, never a guessed image. */}
      <section className="flex flex-col gap-2" aria-label="Logo">
        <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">Logo</h3>
        <div className="flex h-24 items-center justify-center rounded-lg border border-dashed text-caption text-muted-foreground">
          暂无 Logo
        </div>
      </section>

      {fonts.length > 0 ? (
        <section className="flex flex-col gap-3" aria-label="字体排版">
          <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">字体排版</h3>
          {/* Open Design's fontTiles: auto-fill cards ≥140px, an Ag specimen
              area 104px tall at 52px on the page background, name and role in
              the panel footer. */}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-3">
            {fonts.map((font) => (
              <div key={font.role} className="overflow-hidden rounded-lg border bg-card">
                {/* Specimen glyphs, not UI text: the nearest steps above the
                    role ramp to OD's 52px/32px are text-5xl and text-4xl. */}
                <div
                  className="flex h-[104px] items-center justify-center bg-background text-5xl leading-none text-foreground"
                  style={{ fontFamily: font.family }}
                >
                  Ag
                </div>
                <div className="px-3 pb-2.5 pt-2">
                  <span className="block break-words text-caption font-semibold leading-tight">{font.family}</span>
                  <span className="mt-px block text-micro uppercase tracking-wider text-muted-foreground">{font.role}</span>
                </div>
              </div>
            ))}
          </div>
          {/* fontList: one specimen row per role — the role tag and family on
              a baseline, then the sample at 32px, single line, ellipsised. */}
          <div className="flex flex-col gap-3.5">
            {fonts.map((font) => (
              <div key={font.role} className="flex min-w-0 flex-col gap-2">
                <p className="flex min-w-0 items-baseline gap-2.5">
                  <span className="shrink-0 text-micro font-semibold uppercase tracking-wide text-muted-foreground">{font.role}</span>
                  <span className="min-w-0 truncate text-caption font-semibold">
                    {font.family}
                    {weights ? <span className="font-medium text-muted-foreground"> · {weights}</span> : null}
                  </span>
                </p>
                <span
                  className="truncate text-4xl leading-[1.2] text-foreground"
                  style={{ fontFamily: font.family }}
                >
                  {font.sample}
                </span>
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {data.palette.length > 0 ? (
        <section className="flex flex-col gap-3" aria-label="调色板">
          <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">调色板</h3>
          {/* Open Design's paletteGrid: auto-fill swatch cards ≥150px; the
              84px chip prints the hex bottom-left with its own contrast rule,
              then name, role line and the full usage note in the body. */}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3">
            {data.palette.map((entry) => (
              <div key={`${entry.name}-${entry.value}`} className="min-w-0 overflow-hidden rounded-lg border bg-card">
                <span className="flex h-[84px] items-end p-2" style={{ background: entry.value }}>
                  <span
                    className="font-mono text-micro uppercase tabular-nums"
                    style={{ color: isLightHex(entry.value) ? "rgba(0,0,0,.65)" : "rgba(255,255,255,.9)" }}
                  >
                    {entry.value}
                  </span>
                </span>
                <span className="flex min-w-0 flex-col gap-0.5 px-3 pb-3 pt-2">
                  <span className="break-words text-caption font-semibold leading-tight">{entry.name}</span>
                  {entry.role ? (
                    <span className="truncate text-micro lowercase text-muted-foreground">{entry.role}</span>
                  ) : null}
                  {entry.usage ? (
                    <span className="mt-1 break-words text-micro leading-relaxed text-muted-foreground">{entry.usage}</span>
                  ) : null}
                </span>
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {data.layout_guidelines.length > 0 ? (
        <section className="flex flex-col gap-2" aria-label="图像与布局">
          <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">图像与布局</h3>
          <p className="text-caption text-muted-foreground">布局准则</p>
          <ul className="list-disc space-y-1 pl-5 text-body leading-6">
            {data.layout_guidelines.map((guideline) => (
              <li key={guideline}>{guideline}</li>
            ))}
          </ul>
        </section>
      ) : null}

      <BuiltinKitModule system={data} />

      {data.artifacts.length > 0 ? (
        <section className="flex flex-col gap-2" aria-label="设计系统素材">
          <h3 className="text-caption font-semibold tracking-wide text-muted-foreground">设计系统素材</h3>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 2xl:grid-cols-4">
            {data.artifacts.map((artifact) => (
              <BuiltinArtifactCard key={artifact.id} artifact={artifact} systemName={data.name || slug} />
            ))}
          </div>
        </section>
      ) : null}

      <p className="text-caption text-muted-foreground">
        官方体系是只读参考。要在项目里使用，请在项目的「设计体系」中创建，并以它作为参考风格。
      </p>
    </div>
  );
}

/**
 * The 官方 scope. Built-ins carry a slug, a category and no owner, where a
 * saved system carries a UUID, a platform and a project — different enough
 * that sharing one list would mean inventing the fields each is missing.
 */
function BuiltinSystemsPanel({ search }: { search: string }) {
  const wsId = useWorkspaceId();
  const { data: systems = [], isLoading, error, refetch } = useQuery(
    builtinDesignSystemListOptions(wsId),
  );
  const [category, setCategory] = useState<string>(ALL_CATEGORIES);
  const [selectedSlug, setSelectedSlug] = useState("");

  const categories = useMemo(
    () => Array.from(new Set(systems.map((system) => system.category).filter(Boolean))).sort(),
    [systems],
  );
  const activeCategory = categories.includes(category) ? category : ALL_CATEGORIES;
  const query = search.trim().toLowerCase();
  const visible = useMemo(
    () =>
      systems
        .filter((system) => activeCategory === ALL_CATEGORIES || system.category === activeCategory)
        .filter((system) =>
          !query || `${system.name} ${system.category} ${system.description}`.toLowerCase().includes(query),
        ),
    [activeCategory, query, systems],
  );
  const selected = visible.find((system) => system.slug === selectedSlug) ?? visible[0];

  if (isLoading) {
    return (
      <div className="flex flex-col gap-2 px-4 py-2">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-12 w-full rounded-lg" />
        ))}
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
        <p className="text-body font-medium">无法加载官方设计体系</p>
        <Button size="sm" variant="outline" onClick={() => void refetch()}>
          重试
        </Button>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden lg:flex-row">
      <aside className="flex shrink-0 flex-col overflow-hidden border-b lg:w-72 lg:border-b-0 lg:border-r">
        {categories.length > 1 ? (
          <div className="shrink-0 px-3 py-2.5">
            <DesignFilterSelect
              label="官方设计体系分类"
              value={activeCategory}
              allValue={ALL_CATEGORIES}
              allLabel="全部分类"
              allCount={systems.length}
              options={categories.map((item) => ({
                value: item,
                label: item,
                count: systems.filter((system) => system.category === item).length,
              }))}
              onChange={setCategory}
            />
          </div>
        ) : null}
        <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto p-2">
          {visible.map((system) => (
            <BuiltinListItem
              key={system.slug}
              system={system}
              selected={system.slug === selected?.slug}
              onSelect={() => setSelectedSlug(system.slug)}
            />
          ))}
        </div>
      </aside>

      <section className="min-w-0 flex-1 overflow-y-auto p-4 lg:p-5">
        {selected ? (
          <BuiltinSystemDetail slug={selected.slug} />
        ) : (
          <Empty className="border py-12">
            <EmptyHeader>
              <EmptyMedia variant="icon"><Boxes /></EmptyMedia>
              <EmptyTitle>没有匹配的官方设计体系</EmptyTitle>
              <EmptyDescription>换一个关键词，或者清除分类筛选。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </section>
    </div>
  );
}

/**
 * The row's leading mark. Open Design's rows show the brand's real favicon
 * (Google's favicon service keyed by a curated slug→host table) at the full
 * row height with no frame; a slug the table does not cover, or a fetch that
 * fails — offline, blocked — falls back to the palette stripe, exactly as
 * upstream does, never to a generic glyph or a broken image.
 */
function BuiltinRowLogo({ slug, swatches, name }: { slug: string; swatches: string[]; name: string }) {
  const logoURL = builtinDesignSystemLogoURL(slug);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [slug]);
  if (!logoURL || failed) {
    return <RowPaletteMark swatches={swatches} seed={name || slug} />;
  }
  return (
    <img
      src={logoURL}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
      className="size-10 shrink-0 rounded-md object-contain"
    />
  );
}

function BuiltinListItem({
  system,
  selected,
  onSelect,
}: {
  system: BuiltinDesignSystem;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      // Same selected treatment as the team rows: a solid surface plus a
      // primary ring, with the hover compound spelled out so the selected
      // row stays identifiable while it is hovered.
      className={cn(
        "flex w-full items-center gap-2.5 rounded-lg p-2 text-left transition-colors",
        selected
          ? "bg-background font-medium text-foreground shadow-sm ring-1 ring-primary/40 hover:bg-background"
          : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
      )}
    >
      <BuiltinRowLogo slug={system.slug} swatches={system.swatches} name={system.name} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="flex w-full min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-body">{system.name || system.slug}</span>
      </span>
      <span className="w-full truncate text-caption font-normal text-muted-foreground">
        {system.category || "未分类"}
      </span>
      </span>
    </button>
  );
}

/**
 * "新建设计体系", at the library's top right as Open Design places its create
 * action on the design-systems page. Both open a dedicated creation flow; here
 * that is the standalone creation page, where the system belongs to the
 * workspace itself and is not bound to a project.
 */
function CreateDesignSystemButton({ onCreate }: { onCreate: () => void }) {
  return (
    <Button size="sm" onClick={onCreate}>
      <Plus className="size-3.5" />
      新建设计体系
    </Button>
  );
}

/**
 * Design system library (DC-054): the workspace-wide view of saved project
 * design systems. It is a reading and pick-up surface only — systems are
 * created inside a project's own scope, and this library never introduces a
 * workspace default that projects would inherit (DC-052 / migration §4.7).
 */
export function DesignSystemLibrary({
  onOpenSystem,
  onCreate,
}: {
  /** Opens the exact project, repository or standalone scope that owns a system. */
  onOpenSystem: (entry: ProjectDesignSystemCatalogueEntry) => void;
  /** Opens the standalone creation page. */
  onCreate: () => void;
}) {
  const wsId = useWorkspaceId();
  const { data: entries = [], isLoading, error, refetch } = useQuery(
    projectDesignSystemCatalogueOptions(wsId),
  );
  // 官方 opens first: a workspace starts with no saved systems of its own,
  // and the bundled catalogue is the scope that always has something to show.
  const [scope, setScope] = useState<LibraryScope>("official");
  const [scopeSelectedByUser, setScopeSelectedByUser] = useState(false);
  const [platform, setPlatform] = useState<string>(ALL_PLATFORMS);
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState("");

  // The official scope is served by the bundled catalogue, so its count comes
  // from there rather than from the workspace's saved systems.
  const { data: builtinSystems = [] } = useQuery(builtinDesignSystemListOptions(wsId));
  useEffect(() => {
    if (scopeSelectedByUser) return;
    setScope(entries.some((entry) => scopeOf(entry) === "mine") ? "mine" : "official");
  }, [entries, scopeSelectedByUser]);
  const scopeCounts = useMemo(() => {
    const counts: Record<LibraryScope, number> = { mine: 0, team: 0, official: builtinSystems.length };
    for (const entry of entries) counts[scopeOf(entry)] += 1;
    return counts;
  }, [builtinSystems.length, entries]);

  const scoped = useMemo(
    () => entries.filter((entry) => scopeOf(entry) === scope),
    [entries, scope],
  );
  const platforms = useMemo<string[]>(
    () => Array.from(new Set(scoped.map((entry) => entry.platform as string))),
    [scoped],
  );
  // A platform the current scope no longer offers falls back to "all" by
  // derivation, so a refreshed catalogue cannot strand the list on an empty
  // filter.
  const activePlatform = platforms.includes(platform) ? platform : ALL_PLATFORMS;
  const query = search.trim().toLowerCase();
  const visible = useMemo(
    () =>
      scoped
        .filter((entry) => activePlatform === ALL_PLATFORMS || entry.platform === activePlatform)
        .filter((entry) =>
          !query || `${entry.name} ${entry.project_title}`.toLowerCase().includes(query),
        ),
    [activePlatform, query, scoped],
  );
  const selected = visible.find((entry) => entry.id === selectedId) ?? visible[0];

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-2 px-4 py-2.5">
        <div role="group" aria-label="设计体系归属" className="flex flex-wrap items-center gap-1.5">
          {SCOPE_LABELS.map((option) => (
            <DesignFilterPill
              key={option.value}
              label={option.label}
              count={scopeCounts[option.value]}
              selected={scope === option.value}
              onClick={() => {
                setScopeSelectedByUser(true);
                setScope(option.value);
                setPlatform(ALL_PLATFORMS);
                setSelectedId("");
              }}
            />
          ))}
        </div>
        <div className="flex w-full items-center gap-2 sm:w-auto">
          <div className="relative min-w-0 flex-1 sm:w-56 sm:flex-none">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              aria-label="搜索设计体系"
              placeholder="搜索设计体系…"
              className="h-8 bg-card pl-8 text-body"
            />
          </div>
          <CreateDesignSystemButton onCreate={onCreate} />
        </div>
      </div>

      {scope === "official" ? (
        <BuiltinSystemsPanel search={search} />
      ) : isLoading ? (
        <div className="flex flex-col gap-2 px-4 py-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : error ? (
        <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
          <p className="text-body font-medium">无法加载设计体系目录</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>
            重试
          </Button>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden lg:flex-row">
          <aside className="flex shrink-0 flex-col overflow-hidden border-b lg:w-72 lg:border-b-0 lg:border-r">
            {platforms.length > 1 ? (
              <div className="shrink-0 px-3 py-2.5">
                <DesignFilterSelect
                  label="设计体系分类"
                  value={activePlatform}
                  allValue={ALL_PLATFORMS}
                  allLabel="全部分类"
                  allCount={scoped.length}
                  options={platforms.map((item) => ({
                    value: item,
                    label: platformLabel(item),
                    count: scoped.filter((entry) => entry.platform === item).length,
                  }))}
                  onChange={setPlatform}
                />
              </div>
            ) : null}
            <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto p-2">
              {visible.map((entry) => (
                <SystemListItem
                  key={entry.id}
                  entry={entry}
                  selected={entry.id === selected?.id}
                  onSelect={() => setSelectedId(entry.id)}
                />
              ))}
            </div>
          </aside>

          <section className="min-w-0 flex-1 overflow-y-auto p-4 lg:p-5">
            {selected ? (
              <SystemDetail entry={selected} onOpenSystem={onOpenSystem} />
            ) : (
              <Empty className="border py-12">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    {scope === "team" ? <SwatchBook /> : <Boxes />}
                  </EmptyMedia>
                  <EmptyTitle>
                    {query || activePlatform !== ALL_PLATFORMS
                      ? "没有匹配的设计体系"
                      : "这里还没有设计体系"}
                  </EmptyTitle>
                  <EmptyDescription>
                    {query || activePlatform !== ALL_PLATFORMS
                      ? "换一个关键词，或者清除分类筛选。"
                      : SCOPE_EMPTY_COPY[scope]}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
