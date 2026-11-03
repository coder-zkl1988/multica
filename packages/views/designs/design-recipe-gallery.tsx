"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AppWindow,
  AudioLines,
  ChartColumn,
  Copy,
  ExternalLink,
  FileCode,
  Image as ImageIcon,
  LayoutTemplate,
  Maximize2,
  Minimize2,
  Presentation,
  Search,
  Star,
  Video,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { useToggleDesignScenarioRecipeFavorite } from "@multica/core/designs/mutations";
import { designScenarioRecipeListOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import type {
  DesignDocument,
  DesignScenarioRecipe,
  ProjectDesignSystemPlatform,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { DesignFilterPill } from "./design-filter-pill";
import { ProjectPicker } from "../projects/components/project-picker";
import {
  AgentSetting,
  BRIEF_MAX_LENGTH,
  PLATFORM_OPTIONS,
  SettingTrigger,
} from "./design-task-composer";

// Sentinel for "no facet picked". Recipe categories are free-form server
// strings, so the gallery cannot reserve a real value for it.
const ALL_FACET = "__all__";

/**
 * The first filter row: what the recipe produces. Mirrors the Open Design
 * community tab, which sorts its catalogue by artifact mode before anything
 * else — a person looking for a deck never wants prototypes in the way.
 *
 * Every position is a `mode` value the seed carries, so the row is a plain
 * equality over data. `live` has no producer yet and is a real position that
 * is simply empty rather than a hidden one.
 */
type RecipeMode = "deck" | "prototype" | "live" | "image" | "video" | "hyperframes" | "audio";

const RECIPE_MODES: ReadonlyArray<{ value: RecipeMode; label: string; icon: typeof AppWindow }> = [
  { value: "deck", label: "幻灯片", icon: Presentation },
  { value: "prototype", label: "原型", icon: AppWindow },
  { value: "live", label: "实时产物", icon: ChartColumn },
  { value: "image", label: "图片", icon: ImageIcon },
  { value: "video", label: "视频", icon: Video },
  { value: "hyperframes", label: "HyperFrames", icon: LayoutTemplate },
  { value: "audio", label: "音频", icon: AudioLines },
];

/**
 * Row-two order per mode, as Open Design displays it (facets.ts
 * SUBCATEGORY_DISPLAY_ORDER / DECK_COMMERCIAL_ORDER). Prototype leads with
 * what people ask for most; decks lead with the paid scenes. A category not
 * listed keeps its catalogue position behind the ordered ones, so a new seed
 * facet appears rather than vanishes.
 */
const CATEGORY_DISPLAY_ORDER: Partial<Record<RecipeMode, readonly string[]>> = {
  prototype: ["落地页 / 营销", "品牌 / 设计", "数据看板", "应用", "开发者工具", "文档 / 报告"],
  deck: [
    "融资路演", "企业战略", "B2B 销售", "产品管理", "设计打磨",
    "市场增长", "数据与财务", "咨询顾问", "政府与政策", "职业培训",
    "学术研究", "AI 素养", "职业发展", "学生作业", "生活故事",
  ],
  image: ["UI / 产品样机", "品牌 / Logo", "分镜", "社交 / 内容", "头像 / 人像", "插画 / 风格"],
  video: ["动效 / 特效", "社交 / 短视频", "营销 / 产品", "数据 / 解说", "电影感 / 叙事"],
};

function orderCategories(mode: RecipeMode, categories: string[]): string[] {
  const order = CATEGORY_DISPLAY_ORDER[mode];
  if (!order) return categories;
  const rank = (value: string) => {
    const index = order.indexOf(value);
    return index === -1 ? order.length : index;
  };
  return categories
    .map((value, index) => ({ value, index }))
    .sort((a, b) => rank(a.value) - rank(b.value) || a.index - b.index)
    .map((entry) => entry.value);
}

function matchesMode(recipe: DesignScenarioRecipe, mode: RecipeMode): boolean {
  return recipe.mode === mode;
}

/**
 * Artifact this recipe produces. Only `prototype` has a producer in this
 * phase — the server rejects everything else on create — so the switch carries
 * a default branch and the gallery closes the start actions rather than
 * offering a button that is guaranteed to fail.
 */
function modeVisual(mode: string): { icon: typeof AppWindow; label: string } {
  switch (mode) {
    case "prototype":
      return { icon: AppWindow, label: "可运行原型" };
    case "deck":
      return { icon: Presentation, label: "演示文稿" };
    case "image":
      return { icon: ImageIcon, label: "图片" };
    case "video":
      return { icon: Video, label: "视频" };
    case "hyperframes":
      return { icon: LayoutTemplate, label: "HyperFrames" };
    case "live":
      return { icon: ChartColumn, label: "实时产物" };
    case "audio":
      return { icon: AudioLines, label: "音频" };
    default:
      return { icon: LayoutTemplate, label: "其他产物" };
  }
}

function originLabel(origin: string): string {
  switch (origin) {
    case "builtin":
      return "官方";
    case "workspace":
      return "工作区";
    case "community":
      return "社区";
    default:
      return "";
  }
}

function platformLabel(platform: ProjectDesignSystemPlatform | ""): string {
  return PLATFORM_OPTIONS.find((option) => option.value === platform)?.label ?? "";
}

function canStartRecipe(recipe: DesignScenarioRecipe): boolean {
  // Decks run the same page-design pipeline as prototypes — the create gate
  // accepts the mode server-side — so deck recipes are startable too.
  return recipe.mode === "prototype" || recipe.mode === "deck";
}

function uniqueInOrder(values: string[]): string[] {
  return Array.from(new Set(values.filter((value) => value.trim().length > 0)));
}

/**
 * Card cover. Follows Open Design: a built-in recipe's cover is the template's
 * own example output rendered live, not a shipped thumbnail — for prototypes
 * and decks that is `example.html` in a sandboxed frame, for media templates a
 * poster. A recipe with neither gets a composed tile that states what it is,
 * never an empty frame that reads as a failed load.
 *
 * The frame carries no `sandbox` attribute on purpose. The server sandboxes
 * the document itself (CSP `sandbox allow-scripts`): scripts run — a deck is
 * invisible until its `<deck-stage>` runtime defines the element, and the
 * WebGL and HyperFrames examples paint with one — but the example gets an
 * opaque origin, so it cannot read this app's cookies or storage, and the
 * rest of the policy denies it the network. Doing it server-side matters:
 * some embedding environments refuse to fetch a frame that is sandboxed
 * client-side into an opaque origin, and the cover then never loads at all,
 * while a document sandboxed by its own headers loads everywhere.
 * `pointer-events-none` keeps the frame from swallowing the card's click.
 *
 * The URL comes from the listing, not from here: the server composes it with
 * the bundle's content digest in the path, so the browser may cache a cover
 * indefinitely and still never show a stale one — a new build is a new URL.
 * It ends in a slash because the example may reference files beside itself
 * (`assets/deck-stage.js`), and only a directory URL resolves them under the
 * same route.
 */
export function RecipePreview({ recipe }: { recipe: DesignScenarioRecipe }) {
  const { icon: ModeIcon, label: modeLabel } = modeVisual(recipe.mode);
  const facets = [recipe.category, recipe.subcategory].filter(Boolean).join(" · ");
  const previewUrl = recipe.preview_kind && recipe.preview_url ? `${api.getBaseUrl()}${recipe.preview_url}` : "";
  return (
    <div className="relative aspect-[16/10] shrink-0 overflow-hidden border-b bg-muted/40">
      {recipe.preview_kind === "html" && previewUrl ? (
        // The example is authored at desktop width; scaling a 1280-wide frame
        // down keeps the layout the template intended instead of reflowing it
        // into a card-width column that looks like nothing the user will get.
        <div className="pointer-events-none absolute left-0 top-0 h-[250%] w-[250%] origin-top-left scale-[0.4]">
          <iframe
            src={previewUrl}
            title=""
            aria-hidden="true"
            tabIndex={-1}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full border-0 bg-background"
          />
        </div>
      ) : recipe.preview_kind === "poster" && previewUrl ? (
        <img
          src={previewUrl}
          alt=""
          loading="lazy"
          className="h-full w-full object-cover transition-transform group-hover/recipe:scale-[1.02]"
        />
      ) : recipe.preview_path ? (
        <img
          src={recipe.preview_path}
          alt=""
          loading="lazy"
          className="h-full w-full object-cover transition-transform group-hover/recipe:scale-[1.02]"
        />
      ) : (
        <div className="flex h-full w-full flex-col items-center justify-center gap-2 px-4 text-center">
          <span className="flex size-9 items-center justify-center rounded-xl border bg-background text-muted-foreground shadow-sm transition-transform group-hover/recipe:scale-105">
            <ModeIcon className="size-4" />
          </span>
          <span className="line-clamp-1 text-caption text-muted-foreground">
            {facets || modeLabel}
          </span>
        </div>
      )}
    </div>
  );
}

/**
 * The per-card save control. A star rather than a button with a label: the
 * card's action row is already two buttons, and the saved state has to be
 * legible at a glance from the grid. Rendered even while the toggle is in
 * flight — the optimistic patch has already flipped `favorited`, so disabling
 * it would only make the card feel stuck.
 */
function FavoriteButton({
  recipe,
  onToggle,
}: {
  recipe: DesignScenarioRecipe;
  onToggle: (recipe: DesignScenarioRecipe) => void;
}) {
  // A catalogue from a backend older than this feature has no id, so there is
  // nothing to address. Hide the control rather than offer a dead one.
  if (!recipe.id) return null;
  return (
    <Button
      type="button"
      size="icon-sm"
      variant="ghost"
      aria-label={recipe.favorited ? `取消收藏「${recipe.title || recipe.slug}」` : `收藏「${recipe.title || recipe.slug}」`}
      aria-pressed={recipe.favorited}
      title={recipe.favorited ? "取消收藏" : "收藏"}
      onClick={() => onToggle(recipe)}
      className={cn(
        "shrink-0",
        recipe.favorited ? "text-primary hover:text-primary" : "text-muted-foreground",
      )}
    >
      <Star className={cn("size-4", recipe.favorited ? "fill-current" : "")} />
    </Button>
  );
}

function RecipeCard({
  recipe,
  onUseInComposer,
  onStart,
  onOpenDetail,
  onToggleFavorite,
}: {
  recipe: DesignScenarioRecipe;
  onUseInComposer: (recipe: DesignScenarioRecipe) => void;
  onStart: (recipe: DesignScenarioRecipe) => void;
  onOpenDetail: (recipe: DesignScenarioRecipe) => void;
  onToggleFavorite: (recipe: DesignScenarioRecipe) => void;
}) {
  const startable = canStartRecipe(recipe);
  const origin = originLabel(recipe.origin);
  const platform = platformLabel(recipe.platform);
  const facets = uniqueInOrder([recipe.category, recipe.subcategory]);
  const title = recipe.title || recipe.slug;

  return (
    <article className="group/recipe flex h-full min-w-0 flex-col overflow-hidden rounded-xl border bg-card transition-colors hover:border-primary/40">
      {/* The cover opens the detail, as in Open Design's community; the
          buttons below act without opening it. */}
      <button
        type="button"
        className="block w-full cursor-pointer text-left"
        aria-label={`查看「${title}」`}
        onClick={() => onOpenDetail(recipe)}
      >
        <RecipePreview recipe={recipe} />
      </button>
      <div className="flex min-w-0 flex-1 flex-col gap-2 p-3.5">
        <div className="flex min-w-0 items-start justify-between gap-2">
          <h3 className="line-clamp-2 min-w-0 break-words text-body-lg font-medium">
            <button type="button" className="text-left hover:text-primary" onClick={() => onOpenDetail(recipe)}>
              {title}
            </button>
          </h3>
          <div className="flex shrink-0 items-center gap-1">
            {origin ? (
              <Badge variant="secondary" className="shrink-0 px-1.5 text-micro font-normal">
                {origin}
              </Badge>
            ) : null}
            <FavoriteButton recipe={recipe} onToggle={onToggleFavorite} />
          </div>
        </div>
        <p className="line-clamp-3 break-words text-caption text-muted-foreground">
          {recipe.summary || "该配方还没有说明。"}
        </p>
        <div className="flex flex-wrap items-center gap-1.5">
          {facets.map((facet) => (
            <Badge key={facet} variant="outline" className="max-w-40 px-1.5 text-micro font-normal">
              <span className="truncate">{facet}</span>
            </Badge>
          ))}
          {platform ? (
            <Badge variant="outline" className="px-1.5 text-micro font-normal">
              {platform}
            </Badge>
          ) : null}
        </div>
        <div className="mt-auto flex flex-wrap items-center gap-2 pt-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-7"
            disabled={!startable}
            onClick={() => onUseInComposer(recipe)}
          >
            填入首页
          </Button>
          <Button
            type="button"
            size="sm"
            className="h-7"
            disabled={!startable}
            onClick={() => onStart(recipe)}
          >
            直接创建
          </Button>
        </div>
        {startable ? null : (
          <p className="text-caption text-muted-foreground">
            这个配方的产物形态（{modeVisual(recipe.mode).label}）暂时还不能创建。
          </p>
        )}
      </div>
    </article>
  );
}

/**
 * The card's detail, as in Open Design's community: the example rendered live
 * at full size (interactive — the frame is sandboxed by the server's own CSP,
 * not scaled down like the cover), the prompt the recipe carries, and the two
 * ways to act on it. Media recipes show their poster instead.
 */
function RecipeDetailDialog({
  recipe,
  onClose,
  onUseInComposer,
  onStart,
  onToggleFavorite,
}: {
  recipe: DesignScenarioRecipe;
  onClose: () => void;
  onUseInComposer: (recipe: DesignScenarioRecipe) => void;
  onStart: (recipe: DesignScenarioRecipe) => void;
  onToggleFavorite: (recipe: DesignScenarioRecipe) => void;
}) {
  const [fullscreen, setFullscreen] = useState(false);
  const startable = canStartRecipe(recipe);
  const origin = originLabel(recipe.origin);
  const platform = platformLabel(recipe.platform);
  const facets = uniqueInOrder([recipe.category, recipe.subcategory]);
  const { label: modeLabel } = modeVisual(recipe.mode);
  const previewUrl = recipe.preview_kind && recipe.preview_url ? `${api.getBaseUrl()}${recipe.preview_url}` : "";
  const title = recipe.title || recipe.slug;

  const copyPrompt = async () => {
    if (!recipe.prompt) return;
    try {
      await navigator.clipboard?.writeText(recipe.prompt);
      toast.success("已复制提示词");
    } catch {
      toast.error("复制失败，请手动选择文本");
    }
  };

  const preview = previewUrl && recipe.preview_kind === "html" ? (
    <iframe
      src={previewUrl}
      title={`${title} 预览`}
      referrerPolicy="no-referrer"
      className="h-full w-full border-0 bg-background"
    />
  ) : previewUrl && recipe.preview_kind === "poster" ? (
    <img src={previewUrl} alt={`${title} 预览`} className="h-full w-full object-contain" />
  ) : (
    <div className="flex h-full w-full items-center justify-center text-caption text-muted-foreground">
      这个配方没有可预览的示例。
    </div>
  );

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent
        className={cn(
          "flex flex-col gap-0 overflow-hidden p-0",
          fullscreen ? "h-[96vh] w-[96vw] max-w-none sm:max-w-none" : "max-h-[90vh] sm:max-w-5xl",
        )}
      >
        <DialogHeader className="shrink-0 border-b px-5 py-3 pr-12">
          <DialogTitle className="flex min-w-0 flex-wrap items-center gap-2 break-words">
            <span className="min-w-0 truncate">{title}</span>
            {origin ? <Badge variant="secondary" className="px-1.5 text-micro font-normal">{origin}</Badge> : null}
            <Badge variant="outline" className="px-1.5 text-micro font-normal">{modeLabel}</Badge>
          </DialogTitle>
          <DialogDescription className="sr-only">配方详情</DialogDescription>
        </DialogHeader>
        <div className={cn("grid min-h-0 flex-1 overflow-hidden", fullscreen ? "grid-cols-1" : "lg:grid-cols-[minmax(0,1fr)_320px]")}>
          <div className="flex min-h-0 flex-col bg-muted/30">
            <div className="flex shrink-0 items-center justify-end gap-0.5 border-b bg-background px-2 py-1">
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label="在新标签页中打开"
                title="在新标签页中打开"
                disabled={!previewUrl}
                onClick={() => window.open(previewUrl, "_blank", "noopener,noreferrer")}
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </Button>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label={fullscreen ? "退出全屏" : "全屏"}
                title={fullscreen ? "退出全屏" : "全屏"}
                onClick={() => setFullscreen((value) => !value)}
              >
                {fullscreen ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
              </Button>
            </div>
            <div className={cn("min-h-0 flex-1", fullscreen ? "" : "aspect-[16/10] lg:aspect-auto lg:min-h-[420px]")}>{preview}</div>
          </div>
          {fullscreen ? null : (
            <aside className="flex min-h-0 flex-col gap-3 overflow-y-auto border-t p-4 lg:border-l lg:border-t-0">
              {recipe.summary ? <p className="text-body text-muted-foreground">{recipe.summary}</p> : null}
              <div className="flex flex-wrap items-center gap-1.5">
                {facets.map((facet) => (
                  <Badge key={facet} variant="outline" className="max-w-48 px-1.5 text-micro font-normal">
                    <span className="truncate">{facet}</span>
                  </Badge>
                ))}
                {platform ? <Badge variant="outline" className="px-1.5 text-micro font-normal">{platform}</Badge> : null}
              </div>
              {recipe.prompt ? (
                <section className="flex min-h-0 flex-col gap-1.5" aria-label="提示词">
                  <div className="flex items-center justify-between gap-2">
                    <h3 className="text-caption font-medium text-muted-foreground">提示词</h3>
                    <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-caption" onClick={() => void copyPrompt()}>
                      <Copy className="h-3 w-3" />
                      复制
                    </Button>
                  </div>
                  <p className="max-h-64 overflow-y-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2.5 text-caption leading-5">
                    {recipe.prompt}
                  </p>
                </section>
              ) : null}
              <div className="mt-auto flex flex-col gap-2 pt-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={!recipe.id}
                  onClick={() => onToggleFavorite(recipe)}
                >
                  <Star className={cn("size-3.5", recipe.favorited ? "fill-current" : "")} />
                  {recipe.favorited ? "取消收藏" : "收藏"}
                </Button>
                <Button type="button" size="sm" disabled={!startable} onClick={() => { onClose(); onStart(recipe); }}>
                  直接创建
                </Button>
                <Button type="button" size="sm" variant="outline" disabled={!startable} onClick={() => { onClose(); onUseInComposer(recipe); }}>
                  填入首页
                </Button>
                {startable ? null : (
                  <p className="text-caption text-muted-foreground">
                    这个配方的产物形态（{modeLabel}）暂时还不能创建。
                  </p>
                )}
              </div>
            </aside>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Start a page-design task straight from a card. Project and agent are the
 * only fields the server requires that the gallery does not already know, so
 * they are all this dialog asks for on top of the seeded brief (DC-042).
 */
function RecipeStartDialog({
  recipe,
  onClose,
  onStarted,
}: {
  recipe: DesignScenarioRecipe;
  onClose: () => void;
  onStarted: (document: DesignDocument) => void;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [projectId, setProjectId] = useState("");
  const [agentId, setAgentId] = useState("");
  // Seeded from the recipe at mount. The caller keys this dialog by slug, so
  // opening a different card starts from that recipe's prompt rather than
  // from whatever the previous one was edited into.
  const [brief, setBrief] = useState(recipe.prompt);
  // No platform pill here either (the preview page owns device switching):
  // the target platform is the recipe's own suggestion.
  const platform: ProjectDesignSystemPlatform = recipe.platform || "web";

  const trimmedBrief = brief.trim();
  const briefTooLong = brief.length > BRIEF_MAX_LENGTH;

  const startTask = useMutation({
    mutationFn: () =>
      api.createDesignDocument({
        project_id: projectId,
        agent_id: agentId,
        platform,
        recipe: recipe.slug,
        brief: trimmedBrief,
      }),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({
        queryKey: designKeys.documents(wsId, created.project_id || projectId),
      });
      // DC-053: the gallery attaches no repository, and the server's own flag
      // stays the only thing allowed to claim the agent read code.
      toast.success(
        created.repository_grounded === true
          ? "已创建页面设计任务，智能体将对所选仓库做只读取证"
          : "已创建页面设计任务，本次未做仓库取证",
      );
      onStarted(created);
      onClose();
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "创建页面设计任务失败"),
  });

  const missingRequirement = !projectId
    ? "请选择项目"
    : !agentId
      ? "请选择智能体"
      : !trimmedBrief
        ? "请描述你想要的页面"
        : briefTooLong
          ? `需求描述超出 ${BRIEF_MAX_LENGTH} 字上限`
          : "";

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (open || startTask.isPending) return;
        onClose();
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="break-words">
            用「{recipe.title || recipe.slug}」创建页面设计
          </DialogTitle>
          <DialogDescription>
            需求描述来自这个配方，可以直接改。选好项目和智能体就能发起任务。
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap items-center gap-2">
          <ProjectPicker
            projectId={projectId || null}
            onUpdate={(updates) => setProjectId(updates.project_id ?? "")}
            align="start"
            triggerRender={<SettingTrigger filled={!!projectId} aria-label="项目" />}
          />
          <AgentSetting agents={agents} agentId={agentId} onChange={setAgentId} />
        </div>
        <Textarea
          value={brief}
          onChange={(event) => setBrief(event.target.value)}
          aria-label="页面需求描述"
          className="min-h-40 resize-none text-body"
        />
        {/* Says which field is still missing rather than leaving a dead
            button — the pickers sit above the brief, so "disabled" on its own
            does not point anywhere. */}
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <p
            className={cn("text-caption", briefTooLong ? "text-destructive" : "text-muted-foreground")}
          >
            {brief.length} / {BRIEF_MAX_LENGTH}
          </p>
          <p role="status" className="text-caption text-muted-foreground">
            {startTask.isPending ? "" : missingRequirement}
          </p>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={startTask.isPending}>
            取消
          </Button>
          <Button
            type="button"
            disabled={!!missingRequirement || startTask.isPending}
            onClick={() => startTask.mutate()}
          >
            {startTask.isPending ? "创建中…" : "生成页面设计"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Design centre community tab (DC-041 / DC-048): the catalogue of page-design
 * task recipes visible to this workspace. A recipe is a task configuration,
 * not a design asset — a card either seeds the home composer or starts a task
 * of its own, and produces nothing on its own.
 */
export function DesignRecipeGallery({
  onUseInComposer,
  onStarted,
}: {
  /** Hands the recipe to the home composer and switches to that tab. */
  onUseInComposer: (recipe: DesignScenarioRecipe) => void;
  /** Called after the server has created the document, never before. */
  onStarted: (document: DesignDocument) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: recipes = [], isLoading, error, refetch } = useQuery(
    designScenarioRecipeListOptions(wsId),
  );
  const [search, setSearch] = useState("");
  // Empty until the user picks: the effective mode below opens on the first
  // mode with recipes, so the gallery never lands on an empty row. An explicit
  // pick is honoured even when empty — a click that visibly does nothing is
  // worse than an honest empty state — but a picked mode that later loses all
  // its recipes (say the mode row shrinks) still falls back rather than
  // stranding the grid.
  const [pickedMode, setPickedMode] = useState<RecipeMode | "">("");
  const [category, setCategory] = useState(ALL_FACET);
  // Saved is a view filter, not a second request: every row already carries
  // its `favorited` flag from the listing, so a reload restores the same
  // filter with the same data.
  const [savedOnly, setSavedOnly] = useState(false);
  const [startTarget, setStartTarget] = useState<DesignScenarioRecipe | null>(null);
  const [detailTarget, setDetailTarget] = useState<DesignScenarioRecipe | null>(null);
  const toggleFavorite = useToggleDesignScenarioRecipeFavorite();
  const savedCount = useMemo(
    () => recipes.filter((recipe) => recipe.favorited).length,
    [recipes],
  );
  const onToggleFavorite = (recipe: DesignScenarioRecipe) => {
    toggleFavorite.mutate({ recipeId: recipe.id, favorited: !recipe.favorited });
  };
  // The detail dialog is keyed by slug but its props come from state captured
  // when it opened; re-point it at the freshest row so its save button shows
  // the state the optimistic patch already wrote.
  const detailRecipe = detailTarget
    ? recipes.find((recipe) => recipe.id === detailTarget.id) ?? detailTarget
    : null;

  const modeCounts = useMemo(
    () => RECIPE_MODES.map((option) => ({
      ...option,
      count: recipes.filter((recipe) => matchesMode(recipe, option.value)).length,
    })),
    [recipes],
  );
  const mode: RecipeMode =
    pickedMode || (modeCounts.find((option) => option.count > 0)?.value ?? "prototype");
  const modeMatches = useMemo(
    () => recipes.filter((recipe) => matchesMode(recipe, mode)),
    [mode, recipes],
  );
  const categories = useMemo(
    () => orderCategories(mode, uniqueInOrder(modeMatches.map((recipe) => recipe.category))),
    [mode, modeMatches],
  );
  // A facet the catalogue no longer offers falls back to "all" by derivation,
  // so a refreshed catalogue can never strand the grid on an empty filter.
  const activeCategory = categories.includes(category) ? category : ALL_FACET;
  const categoryMatches = useMemo(
    () => activeCategory === ALL_FACET
      ? modeMatches
      : modeMatches.filter((recipe) => recipe.category === activeCategory),
    [activeCategory, modeMatches],
  );

  const query = search.trim().toLowerCase();
  const filtered = useMemo(() => {
    const scoped = savedOnly
      ? categoryMatches.filter((recipe) => recipe.favorited)
      : categoryMatches;
    if (!query) return scoped;
    return scoped.filter((recipe) => [
      recipe.title,
      recipe.summary,
      recipe.category,
      recipe.subcategory,
      recipe.slug,
    ].join(" ").toLowerCase().includes(query));
  }, [categoryMatches, query, savedOnly]);

  const filtersApplied = activeCategory !== ALL_FACET || !!query || savedOnly;
  const clearFilters = () => {
    setSearch("");
    setCategory(ALL_FACET);
    setSavedOnly(false);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      {/* Full width, no heading: the tab label already names the page, and the
          grid fills whatever width it gets with as many cards as fit — the
          same auto-fill ramp Open Design's community grid uses (240 → 260 →
          280px minimum per card as the viewport grows). */}
      <div className="flex w-full flex-col px-4 py-4 sm:px-6 sm:py-5">
        {isLoading ? (
          <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))] min-[1100px]:grid-cols-[repeat(auto-fill,minmax(260px,1fr))] min-[1400px]:grid-cols-[repeat(auto-fill,minmax(280px,1fr))]">
            {Array.from({ length: 6 }).map((_, index) => (
              <Skeleton key={index} className="h-64 w-full rounded-xl" />
            ))}
          </div>
        ) : error ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-6 py-12 text-center">
            <p className="text-body font-medium">无法加载社区配方</p>
            <p className="text-body text-muted-foreground">请稍后重试。</p>
            <Button size="sm" variant="outline" onClick={() => void refetch()}>
              重试
            </Button>
          </div>
        ) : recipes.length === 0 ? (
          // An empty catalogue is a legitimate answer, not a failure: no
          // official recipe has been published and this workspace has none of
          // its own. Saying so beats an empty panel.
          <Empty className="border py-14">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileCode />
              </EmptyMedia>
              <EmptyTitle>社区还没有可用的配方</EmptyTitle>
              <EmptyDescription>
                官方配方上线，或者工作区里有人发布了自己的配方之后，都会出现在这里。在那之前，可以直接在首页描述你想要的页面。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
                {/* Row one, as in Open Design: what the recipe produces. */}
                <div role="group" aria-label="产物形态" className="flex flex-wrap items-center gap-1.5">
                  {modeCounts.map((option) => (
                    <DesignFilterPill
                      key={option.value}
                      label={option.label}
                      icon={option.icon}
                      count={option.count}
                      selected={mode === option.value}
                      onClick={() => {
                        setPickedMode(option.value);
                        setCategory(ALL_FACET);
                      }}
                    />
                  ))}
                </div>
                <div className="relative w-full sm:w-64">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    aria-label="搜索社区配方"
                    placeholder="搜索…"
                    className="h-8 pl-8 text-body"
                  />
                </div>
              </div>
              {/* Row two: saved first, then the categories inside the chosen
                  mode. The category list is hidden when a mode has only one,
                  since a lone pill beside 全部 says nothing — but the saved
                  chip stays, because a personal bookmark filter is useful on
                  its own, and its count is the account's total saved. */}
              <div className="flex flex-wrap items-center gap-1.5">
                <div role="group" aria-label="收藏筛选">
                  <DesignFilterPill
                    label="收藏"
                    icon={Star}
                    count={savedCount}
                    selected={savedOnly}
                    onClick={() => setSavedOnly((value) => !value)}
                  />
                </div>
                {categories.length > 1 ? (
                  <div role="group" aria-label="配方分类" className="flex flex-wrap items-center gap-1.5">
                    <DesignFilterPill
                      label="全部"
                      count={modeMatches.length}
                      selected={activeCategory === ALL_FACET}
                      onClick={() => setCategory(ALL_FACET)}
                    />
                    {categories.map((item) => (
                      <DesignFilterPill
                        key={item}
                        label={item}
                        count={modeMatches.filter((recipe) => recipe.category === item).length}
                        selected={activeCategory === item}
                        onClick={() => setCategory(item)}
                      />
                    ))}
                  </div>
                ) : null}
              </div>
            </div>

            {filtered.length === 0 ? (
              <Empty className="mt-6 border py-12">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <Search />
                  </EmptyMedia>
                  <EmptyTitle>
                    {savedOnly && savedCount === 0 ? "还没有收藏的配方" : "没有匹配的配方"}
                  </EmptyTitle>
                  <EmptyDescription>
                    {query
                      ? "换一个关键词，或者清除筛选看看这一类的全部配方。"
                      : savedOnly && savedCount === 0
                        ? "在卡片上点星标就能把配方收藏到这里，收藏会保存在你的账号里。"
                        : savedOnly
                          // Saved recipes exist, just not under the facets the
                          // user is scoped to — say so instead of implying
                          // nothing has ever been saved.
                          ? "当前的产物形态和分类下没有收藏的配方，切换筛选或者清除筛选看看。"
                          : "这一类还没有配方。"}
                  </EmptyDescription>
                </EmptyHeader>
                {filtersApplied ? (
                  <EmptyContent>
                    <Button type="button" size="sm" variant="outline" onClick={clearFilters}>
                      清除筛选
                    </Button>
                  </EmptyContent>
                ) : null}
              </Empty>
            ) : (
              <div className="mt-5 grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))] min-[1100px]:grid-cols-[repeat(auto-fill,minmax(260px,1fr))] min-[1400px]:grid-cols-[repeat(auto-fill,minmax(280px,1fr))]">
                {filtered.map((recipe) => (
                  <RecipeCard
                    key={recipe.slug}
                    recipe={recipe}
                    onUseInComposer={onUseInComposer}
                    onStart={setStartTarget}
                    onOpenDetail={setDetailTarget}
                    onToggleFavorite={onToggleFavorite}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>
      {detailRecipe ? (
        <RecipeDetailDialog
          key={detailRecipe.slug}
          recipe={detailRecipe}
          onClose={() => setDetailTarget(null)}
          onUseInComposer={onUseInComposer}
          onStart={setStartTarget}
          onToggleFavorite={onToggleFavorite}
        />
      ) : null}
      {startTarget ? (
        <RecipeStartDialog
          key={startTarget.slug}
          recipe={startTarget}
          onClose={() => setStartTarget(null)}
          onStarted={onStarted}
        />
      ) : null}
    </div>
  );
}
