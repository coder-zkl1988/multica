"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AppWindow,
  ArrowUp,
  AudioLines,
  Bot,
  ChartColumn,
  Check,
  ChevronDown,
  CircleDashed,
  FileCode,
  FileText,
  Frame,
  GitBranch,
  Globe,
  Image as ImageIcon,
  LayoutTemplate,
  ListTodo,
  LoaderCircle,
  Map as MapIcon,
  MessageCircleQuestion,
  Palette,
  Paperclip,
  Plus,
  Presentation,
  Sparkles,
  SwatchBook,
  Video,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { designKeys } from "@multica/core/designs/keys";
import {
  builtinDesignSystemListOptions,
  projectDesignSystemCatalogueOptions,
} from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { issueDetailOptions, projectOpenIssuesOptions } from "@multica/core/issues/queries";
import { projectResourcesOptions } from "@multica/core/projects";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import type {
  Agent,
  BuiltinDesignSystem,
  DesignDocument,
  DesignDocumentRecipe,
  DesignScenarioRecipe,
  Issue,
  ProjectDesignSystemCatalogueEntry,
  ProjectDesignSystemPlatform,
  ProjectResource,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import {
  PickerEmpty,
  PickerItem,
  PropertyPicker,
} from "../issues/components/pickers/property-picker";
import { ProjectPicker } from "../projects/components/project-picker";
import { StatusIcon } from "../issues/components/status-icon";
import { DesignDotGrid } from "./design-dot-grid";
import { DesignExamplePrompts, PROTOTYPE_FAMILY } from "./design-example-prompts";
import { DesignRecentDocuments } from "./design-recent-documents";
import {
  PLACEHOLDER_BRIEF_EXAMPLES,
  useTypewriterPlaceholder,
} from "./typewriter-placeholder";

/**
 * The creation rail (DC-049). Open Design's fixed ten top-level output types
 * in its exact order (home-hero/chips.ts CREATE_RAIL_ORDER), live and
 * not-yet-live together, so the rail reads as the whole product surface
 * rather than two stacked tiers. Not here on purpose: 线框图 and 移动应用
 * are 原型's second-level scenes on the example wall's row (see
 * design-example-prompts.tsx); 来自 Figma is a migration action living in
 * the composer's + menu, mirroring upstream's migrate group; and creating a
 * design system has its own entry point on the 设计体系 tab (DC-052 /
 * DC-054).
 *
 * A chip carrying a `recipe` starts a real page-design task; the recipe is the
 * configuration the agent follows, not a different artifact kind. Picking none
 * leaves the recipe at `default`, the free-form path.
 *
 * A chip without one carries `unavailable` instead, and says what is actually
 * missing. They are not one queue: a deck and a long-form document were only
 * ever recipes over the pipeline that already exists, so they run; an image,
 * a video and an audio piece need a deliverable this package cannot hold; and
 * a live artifact is not unbuilt at all — it contradicts the rule that makes
 * a prototype safe to preview, which is that it runs with the network off.
 * "即将支持" on all of them alike told the user the same untrue thing.
 */
const CREATE_TYPES: ReadonlyArray<{
  id: string;
  recipe?: Exclude<DesignDocumentRecipe, "default">;
  label: string;
  description: string;
  /** Why this cannot be picked. Present exactly when `recipe` is absent. */
  unavailable?: string;
  icon: typeof AppWindow;
}> = [
  { id: "ui-mockup", recipe: "ui-mockup", label: "原型", description: "可交互的应用界面稿", icon: AppWindow },
  { id: "deck", recipe: "deck", label: "幻灯片", description: "成套的演示页面", icon: Presentation },
  { id: "image", label: "图片", description: "单张视觉素材", unavailable: "产物必须包含可交互的原型页面，单张素材还没有对应的产物形态", icon: ImageIcon },
  { id: "document", recipe: "long-form", label: "文档", description: "长文与报告版式", icon: FileText },
  { id: "hyperframes", label: "HyperFrames", description: "HTML 连续帧动画合成", unavailable: "逐帧合成的产物契约尚未定义", icon: LayoutTemplate },
  { id: "web-clone", recipe: "web-clone", label: "网站复刻", description: "按现有网站还原页面", icon: Globe },
  { id: "video", label: "视频", description: "分镜与动态素材", unavailable: "需要视频生成能力，设计包也无法承载视频产物", icon: Video },
  { id: "audio", label: "音频", description: "语音与音效素材", unavailable: "需要音频生成能力，设计包也无法承载音频产物", icon: AudioLines },
  { id: "live-board", label: "实时产物", description: "可刷新、接入数据的实时页面", unavailable: "原型必须在断网下运行，接入实时数据与这条规则冲突", icon: ChartColumn },
  { id: "webgl", label: "WebGL 体验", description: "三维与实时渲染", unavailable: "离线规则下不能引入渲染库，需要自带实现，尚未支持", icon: Sparkles },
];

export const PLATFORM_OPTIONS: ReadonlyArray<{
  value: ProjectDesignSystemPlatform;
  label: string;
}> = [
  { value: "web", label: "Web" },
  { value: "mobile", label: "移动端" },
  { value: "cross_platform", label: "跨端" },
];

/**
 * Open Design's composer modes. 设计 is the artifact path this composer has
 * always been; 规划 and 提问 hand the prompt to an agent CHAT instead — a
 * planning conversation whose output the user brings back here, or a plain
 * question that must not create a design document at all.
 */
type ComposerMode = "design" | "plan" | "ask";

const COMPOSER_MODES: ReadonlyArray<{
  id: ComposerMode;
  label: string;
  description: string;
  icon: typeof Sparkles;
}> = [
  { id: "plan", label: "规划", description: "先和智能体产出可编辑的规划，确认后再回到这里生成设计稿。", icon: MapIcon },
  { id: "design", label: "设计", description: "创建具体的设计产物：页面原型、线框图、移动应用界面等。", icon: Sparkles },
  { id: "ask", label: "提问", description: "快速问答、修改建议和讨论，不产出新的设计稿。", icon: MessageCircleQuestion },
];

/**
 * The planning instruction 规划 mode wraps around the user's words. Ends by
 * asking for a ready-to-paste 需求描述, which is the honest bridge back to
 * 设计 mode until a structured plan-to-generate hand-off exists.
 */
export function planInstruction(brief: string): string {
  return [
    "请为下面的设计需求产出一份可讨论、可修改的设计规划，包含：目标与受众、页面清单、关键流程、需要覆盖的状态与边界情况、开放问题。",
    "最后单独给出一段可以直接粘贴到设计稿需求描述里的最终版本。",
    "",
    brief,
  ].join("\n");
}

// Mirrors `designDocumentMaxBriefBytes` on the server. Counted in characters
// here, so a Chinese brief hits this well before the byte limit — the point is
// to warn early, not to reproduce the server's arithmetic.
export const BRIEF_MAX_LENGTH = 4000;
/** Mirrors the server's design document attachment cap. */
const MAX_ATTACHMENTS = 8;
/** The placeholder shown whenever the typewriter rotation is off — plan/ask
 *  modes and any moment the examples are unavailable. Exported for the
 *  composer suite, which asserts the fallback without copying the copy. */
export const STATIC_BRIEF_PLACEHOLDER = "例如：做一个 CRM 客户列表页，支持筛选、批量操作和客户详情抽屉。";

/**
 * A recipe the community gallery handed to the composer (DC-041). Carries its
 * own token so picking the same card twice re-seeds the brief: without it the
 * second click would be a no-op, since the recipe itself has not changed.
 */
export interface DesignRecipeSelection {
  token: number;
  recipe: DesignScenarioRecipe;
}

export interface DesignTaskComposerInitialContext {
  projectId: string;
  issueId: string;
  agentId?: string;
}

function repositoryUrl(resource: ProjectResource): string {
  const ref = resource.resource_ref as { url?: unknown } | undefined;
  return typeof ref?.url === "string" ? ref.url.trim() : "";
}

function repositoryLabel(resource: ProjectResource): string {
  const label = resource.label?.trim();
  if (label) return label;
  const url = repositoryUrl(resource);
  if (!url) return "未命名仓库";
  const normalized = url.replace(/\.git$/, "").replace(/\/+$/, "");
  return normalized.split("/").pop() || normalized;
}

export function matchesQuery(haystack: string, query: string): boolean {
  return !query || haystack.toLowerCase().includes(query);
}

/**
 * Shared trigger for the settings row. A field that is set reads as
 * foreground text; an unset one stays muted, so required-but-empty fields are
 * visible without painting them as errors before the user has done anything.
 */
export function SettingTrigger({
  filled,
  className,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { filled?: boolean }) {
  return (
    <button
      type="button"
      className={cn(
        // Opaque on purpose: the composer sits on the dot grid, and a
        // transparent control lets the pattern read through the button, which
        // both muddies the label and hides where the hit area actually is.
        "flex min-w-0 max-w-64 cursor-pointer items-center gap-1.5 rounded-full border bg-card px-2.5 py-1 text-caption transition-colors",
        "hover:bg-accent data-popup-open:bg-accent data-popup-open:text-accent-foreground",
        "disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:bg-card",
        filled ? "text-foreground" : "text-muted-foreground",
        className,
      )}
      {...props}
    >
      {children}
      <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
    </button>
  );
}

/**
 * One position in the creation rail. Compact by design: the rail has to read
 * as a complete surface at a glance, so the scenario's longer wording lives in
 * the tooltip. A position with nothing behind it is disabled and says so in
 * its accessible name — never a live-looking control that does nothing.
 */
function CreateChip({
  label,
  description,
  icon: Icon,
  selected,
  unavailable,
  onClick,
}: {
  label: string;
  description: string;
  icon: typeof AppWindow;
  selected?: boolean;
  /** Why this chip cannot be picked; absent means it can. */
  unavailable?: string;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      // Omitted for chips that navigate rather than toggle a recipe — a
      // pressed state they can never enter would only mislead.
      aria-pressed={unavailable || selected === undefined ? undefined : selected}
      aria-label={unavailable ? `${label}（暂不可用：${unavailable}）` : undefined}
      disabled={!!unavailable}
      title={unavailable ? `${description}\n暂不可用：${unavailable}` : description}
      onClick={onClick}
      className={cn(
        // Same reason as SettingTrigger: opaque so the dot grid cannot show
        // through the chip.
        "flex h-7 min-w-0 shrink-0 items-center gap-1.5 rounded-full border bg-card px-2.5 text-caption transition-colors",
        unavailable
          ? "cursor-not-allowed border-dashed bg-muted text-muted-foreground"
          : "cursor-pointer text-muted-foreground hover:bg-accent hover:text-foreground",
        // Selection has to survive hover, so it lives on border, surface,
        // weight and colour — dimensions hover never touches — and the hover
        // compound is spelled out so a selected chip cannot visually downgrade
        // to a plain hover.
        !unavailable && selected
          ? "border-primary bg-accent font-medium text-primary hover:border-primary hover:bg-accent hover:text-primary"
          : undefined,
      )}
    >
      <Icon className="size-3.5 shrink-0" />
      <span className="truncate">{label}</span>
    </button>
  );
}

/**
 * The creation rail: Open Design's fixed ten, all visible. The row wraps on
 * narrow widths instead of folding into a menu — with the list capped at ten
 * there is nothing worth hiding, and a rail that always shows every type
 * reads as the complete surface at a glance.
 */
function CreateTypeRail({
  recipe,
  onPick,
}: {
  // Widened the same way the composer's own state is: a catalogue recipe is a
  // slug, and it has to leave every built-in chip unselected rather than fail
  // to type-check against the union.
  recipe: DesignDocumentRecipe | string;
  onPick: (recipe: Exclude<DesignDocumentRecipe, "default">) => void;
}) {
  return (
    <div role="group" aria-label="设计场景" className="flex flex-wrap items-center gap-1.5">
      {CREATE_TYPES.map((type) => (
        <CreateChip
          key={type.id}
          label={type.label}
          description={type.description}
          icon={type.icon}
          // 原型 reads as selected for its whole family — plain or refined
          // into a scene on the example wall's row — so the rail never looks
          // empty while a scene recipe is what is actually armed.
          selected={
            type.recipe
              ? type.id === "ui-mockup"
                ? PROTOTYPE_FAMILY.has(recipe)
                : recipe === type.recipe
              : undefined
          }
          {...(type.unavailable ? { unavailable: type.unavailable } : {})}
          onClick={type.recipe ? () => onPick(type.recipe!) : undefined}
        />
      ))}
    </div>
  );
}

export function AgentSetting({
  agents,
  agentId,
  onChange,
}: {
  agents: Agent[];
  agentId: string;
  onChange: (agentId: string) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const active = useMemo(() => agents.filter((agent) => !agent.archived_at), [agents]);
  const selected = active.find((agent) => agent.id === agentId);
  const query = filter.trim().toLowerCase();
  const filtered = active.filter((agent) => matchesQuery(agent.name, query));

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.design_delivery.search_agents)}
      onSearchChange={setFilter}
      triggerRender={<SettingTrigger filled={!!selected} aria-label={t(($) => $.design_delivery.agent)} />}
      trigger={
        selected ? (
          <>
            <ActorAvatar actorType="agent" actorId={selected.id} size="sm" showStatusDot />
            <span className="truncate">{selected.name}</span>
          </>
        ) : (
          <>
            <Bot className="size-3.5 shrink-0" />
            <span className="truncate">{t(($) => $.design_delivery.select_agent)}</span>
          </>
        )
      }
    >
      {filtered.length === 0 ? (
        <PickerEmpty />
      ) : (
        filtered.map((agent) => {
          // An agent without a runtime cannot pick the task up, so offering it
          // would only produce a task that never starts.
          const runtimeBound = isAgentRuntimeBound(agent);
          return (
            <PickerItem
              key={agent.id}
              selected={agent.id === agentId}
              disabled={!runtimeBound}
              tooltip={runtimeBound ? undefined : t(($) => $.design_delivery.agent_unbound)}
              onClick={() => {
                onChange(agent.id);
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
              <span className="truncate">{agent.name}</span>
            </PickerItem>
          );
        })
      )}
    </PropertyPicker>
  );
}

/**
 * Repository scope (DC-053). Attaching a repository grounds the task against
 * it; "不指定仓库" is a first-class choice, not an empty state — so it is a
 * named row rather than a clear affordance, and the copy below the row spells
 * out what each choice means for the result.
 */
export function RepositorySetting({
  repositories,
  repositoryId,
  disabled,
  onChange,
  required = false,
}: {
  repositories: ProjectResource[];
  repositoryId: string;
  disabled: boolean;
  onChange: (repositoryId: string) => void;
  required?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const selected = repositories.find((repository) => repository.id === repositoryId);
  const query = filter.trim().toLowerCase();
  const filtered = repositories.filter((repository) =>
    matchesQuery(`${repositoryLabel(repository)} ${repositoryUrl(repository)}`, query),
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.design_delivery.search_repositories)}
      onSearchChange={setFilter}
      triggerRender={
        <SettingTrigger filled={!!selected} disabled={disabled} aria-label={t(($) => $.design_delivery.repository)} />
      }
      trigger={
        selected ? (
          <>
            <GitBranch className="size-3.5 shrink-0" />
            <span className="truncate">{repositoryLabel(selected)}</span>
          </>
        ) : (
          <>
            <GitBranch className="size-3.5 shrink-0" />
            <span className="truncate">{required ? t(($) => $.design_delivery.select_repository) : t(($) => $.design_delivery.no_repository)}</span>
          </>
        )
      }
    >
      {!required && <PickerItem
        selected={!repositoryId}
        onClick={() => {
          onChange("");
          setOpen(false);
        }}
      >
        <CircleDashed className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{t(($) => $.design_delivery.no_repository)}</span>
      </PickerItem>}
      {filtered.map((repository) => (
        <PickerItem
          key={repository.id}
          selected={repository.id === repositoryId}
          tooltip={repositoryUrl(repository) || undefined}
          onClick={() => {
            onChange(repository.id);
            setOpen(false);
          }}
        >
          <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{repositoryLabel(repository)}</span>
        </PickerItem>
      ))}
      {repositories.length === 0 ? (
        <div className="px-2 py-1.5 text-caption text-muted-foreground">
          {t(($) => $.design_delivery.repositories_empty)}
        </div>
      ) : null}
      {repositories.length > 0 && filtered.length === 0 && query ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}

/**
 * Optional issue link (DC-045). Linking is traceability only — it never moves
 * the issue's status, assignee or priority — so an unlinked design document is
 * an ordinary exploratory one, not an incomplete one.
 */
export function IssueSetting({
  issues,
  issueId,
  disabled,
  onChange,
  label = "关联任务",
  emptyLabel = "不关联任务",
}: {
  issues: Issue[];
  issueId: string;
  disabled: boolean;
  onChange: (issueId: string) => void;
  /** What the control is for; the workbench uses it to deliver a design. */
  label?: string;
  emptyLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const selected = issues.find((issue) => issue.id === issueId);
  const query = filter.trim().toLowerCase();
  const filtered = issues.filter((issue) =>
    matchesQuery(`${issue.identifier} ${issue.title}`, query),
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder="搜索任务…"
      onSearchChange={setFilter}
      triggerRender={
        <SettingTrigger filled={!!selected} disabled={disabled} aria-label={label} />
      }
      trigger={
        selected ? (
          <>
            <StatusIcon status={selected.status} className="size-3.5 shrink-0" />
            <span className="truncate">{selected.identifier}</span>
          </>
        ) : (
          <>
            <ListTodo className="size-3.5 shrink-0" />
            <span className="truncate">{emptyLabel}</span>
          </>
        )
      }
    >
      <PickerItem
        emptyValue
        selected={!issueId}
        onClick={() => {
          onChange("");
          setOpen(false);
        }}
      >
        <CircleDashed className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate text-muted-foreground">不关联任务</span>
      </PickerItem>
      {filtered.map((issue) => (
        <PickerItem
          key={issue.id}
          selected={issue.id === issueId}
          tooltip={issue.title}
          onClick={() => {
            onChange(issue.id);
            setOpen(false);
          }}
        >
          <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
          <span className="shrink-0 font-mono text-caption text-muted-foreground">
            {issue.identifier}
          </span>
          <span className="truncate">{issue.title}</span>
        </PickerItem>
      ))}
      {issues.length === 0 ? (
        <div className="px-2 py-1.5 text-caption text-muted-foreground">
          当前项目没有未完成的任务。
        </div>
      ) : null}
      {issues.length > 0 && filtered.length === 0 && query ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}

/**
 * The launcher's single control for what happens to the task-card side of a
 * run: nothing, a new companion card, or a link to an existing one. These
 * used to be two separate chips — a picker for an existing issue, plus a
 * toggle for creating one — which read as unrelated settings even though
 * only one of the three outcomes can ever be true at once.
 *
 * The companion card is a traceable record, never a driver: naming an
 * existing issue here does not move it, and a created one is not either
 * (DC-045).
 */
export function IssueDestinationSetting({
  issues,
  issueId,
  createIssue,
  disabled,
  onChangeIssue,
  onChangeCreate,
}: {
  issues: Issue[];
  issueId: string;
  /** Ignored once `issueId` is set — an existing issue already answers this. */
  createIssue: boolean;
  disabled: boolean;
  onChangeIssue: (issueId: string) => void;
  onChangeCreate: (createIssue: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const selected = issues.find((issue) => issue.id === issueId);
  const query = filter.trim().toLowerCase();
  const filtered = issues.filter((issue) =>
    matchesQuery(`${issue.identifier} ${issue.title}`, query),
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder="搜索任务…"
      onSearchChange={setFilter}
      triggerRender={
        <SettingTrigger filled={!!selected || createIssue} disabled={disabled} aria-label="任务卡片" />
      }
      trigger={
        selected ? (
          <>
            <StatusIcon status={selected.status} className="size-3.5 shrink-0" />
            <span className="truncate">{selected.identifier}</span>
          </>
        ) : createIssue ? (
          <>
            <Plus className="size-3.5 shrink-0" />
            <span className="truncate">同步创建任务</span>
          </>
        ) : (
          <>
            <ListTodo className="size-3.5 shrink-0" />
            <span className="truncate">不创建任务</span>
          </>
        )
      }
    >
      <PickerItem
        emptyValue
        selected={!issueId && !createIssue}
        onClick={() => {
          onChangeIssue("");
          onChangeCreate(false);
          setOpen(false);
        }}
      >
        <CircleDashed className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate text-muted-foreground">不创建任务</span>
      </PickerItem>
      <PickerItem
        selected={!issueId && createIssue}
        onClick={() => {
          onChangeIssue("");
          onChangeCreate(true);
          setOpen(false);
        }}
      >
        <Plus className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">同步创建任务</span>
      </PickerItem>
      {/* The scope belongs on the header, not only in the empty state: this
          list is open issues, and a project whose other tasks are done showed
          one row out of three with nothing saying why — which reads as a list
          that failed to refresh rather than one that is doing its job. */}
      <div className="flex items-baseline justify-between gap-2 px-2 pb-1 pt-2 text-caption text-muted-foreground">
        <span>关联已有任务</span>
        <span>仅未完成</span>
      </div>
      {filtered.map((issue) => (
        <PickerItem
          key={issue.id}
          selected={issue.id === issueId}
          tooltip={issue.title}
          onClick={() => {
            onChangeIssue(issue.id);
            onChangeCreate(false);
            setOpen(false);
          }}
        >
          <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
          <span className="shrink-0 font-mono text-caption text-muted-foreground">
            {issue.identifier}
          </span>
          <span className="truncate">{issue.title}</span>
        </PickerItem>
      ))}
      {issues.length === 0 ? (
        <div className="px-2 py-1.5 text-caption text-muted-foreground">
          当前项目没有未完成的任务。
        </div>
      ) : null}
      {issues.length > 0 && filtered.length === 0 && query ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}

/**
 * The design system this run must design under (DC-060). Open Design's home
 * carries the same picker: design systems are workspace platform material, so
 * any saved system — or a bundled catalogue preset — can govern a page design,
 * independent of which project the document belongs to.
 *
 * "不指定设计体系" is a real choice, not an empty state: it hands the run to the
 * repository -> project fallback, which is how every design task worked before
 * this picker existed.
 */
export function DesignSystemSetting({
  workspaceSystems,
  builtinSystems,
  designSystemId,
  builtinSlug,
  onChange,
}: {
  workspaceSystems: ProjectDesignSystemCatalogueEntry[];
  builtinSystems: BuiltinDesignSystem[];
  designSystemId: string;
  builtinSlug: string;
  onChange: (selection: { designSystemId: string; builtinSlug: string }) => void;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();
  const selectedWorkspace = workspaceSystems.find((system) => system.id === designSystemId);
  const selectedBuiltin = builtinSystems.find((system) => system.slug === builtinSlug);
  const filteredWorkspace = workspaceSystems.filter((system) =>
    matchesQuery(`${system.name} ${system.project_title} ${system.summary}`, query),
  );
  // The catalogue is 150+ entries; unsearched it would bury 你的体系, so it
  // shows a head slice until the user types.
  const filteredBuiltin = query
    ? builtinSystems.filter((system) => matchesQuery(`${system.name} ${system.category} ${system.slug}`, query))
    : builtinSystems.slice(0, 12);

  const pick = (selection: { designSystemId: string; builtinSlug: string }) => {
    onChange(selection);
    setOpen(false);
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.design_delivery.search_systems)}
      onSearchChange={setFilter}
      triggerRender={
        <SettingTrigger filled={!!selectedWorkspace || !!selectedBuiltin} aria-label={t(($) => $.design_delivery.system)} />
      }
      trigger={
        <>
          <Palette className="size-3.5 shrink-0" />
          <span className="truncate">
            {selectedWorkspace?.name ?? selectedBuiltin?.name ?? t(($) => $.design_delivery.no_system)}
          </span>
        </>
      }
    >
      <PickerItem
        emptyValue
        selected={!designSystemId && !builtinSlug}
        onClick={() => pick({ designSystemId: "", builtinSlug: "" })}
      >
        <CircleDashed className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate text-muted-foreground">{t(($) => $.design_delivery.no_system)}</span>
      </PickerItem>
      {filteredWorkspace.length > 0 ? (
        <div className="px-2 pb-1 pt-2 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.design_delivery.your_systems)}
        </div>
      ) : null}
      {filteredWorkspace.map((system) => (
        <PickerItem
          key={system.id}
          selected={system.id === designSystemId}
          tooltip={system.summary || undefined}
          onClick={() => pick({ designSystemId: system.id, builtinSlug: "" })}
        >
          <SwatchBook className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{system.name}</span>
        </PickerItem>
      ))}
      {filteredBuiltin.length > 0 ? (
        <div className="px-2 pb-1 pt-2 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.design_delivery.builtin_systems)}
        </div>
      ) : null}
      {filteredBuiltin.map((system) => (
        <PickerItem
          key={system.slug}
          selected={system.slug === builtinSlug}
          tooltip={system.category || undefined}
          onClick={() => pick({ designSystemId: "", builtinSlug: system.slug })}
        >
          {system.swatches.length > 0 ? (
            <span aria-hidden="true" className="size-3.5 shrink-0 rounded-full border" style={{ background: system.swatches[3] ?? system.swatches[0] }} />
          ) : (
            <SwatchBook className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{system.name}</span>
        </PickerItem>
      ))}
      {query && filteredWorkspace.length === 0 && filteredBuiltin.length === 0 ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}

/**
 * Design centre home composer: the cross-project entry point for page-design
 * tasks (DC-042 / DC-049). Project and agent are required; repository, issue
 * and title are not (DC-053 / DC-045).
 */
export function DesignTaskComposer({
  onCreated,
  onBrowseRecipes,
  onOpenDocument,
  recipeSelection,
  initialContext,
}: {
  /** Called after the server has created the document, never before. */
  onCreated: (document: DesignDocument) => void;
  /** Opens the community gallery. Absent hides the community entry. */
  onBrowseRecipes?: () => void;
  /** Opens a project tab, where that project's design files live. */
  onOpenDocument?: (document: DesignDocument) => void;
  /** A recipe picked in the community gallery, waiting to be applied. */
  recipeSelection?: DesignRecipeSelection | null;
  /** Issue entry-point context. Project and issue stay fixed for traceability. */
  initialContext?: DesignTaskComposerInitialContext;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const [mode, setMode] = useState<ComposerMode>("design");
  const [brief, setBrief] = useState("");
  // Widened past the built-in chips on purpose: a community recipe contributes
  // its slug here, and the server validates it against the catalogue.
  const [recipe, setRecipe] = useState<DesignDocumentRecipe | string>("default");
  const [appliedRecipe, setAppliedRecipe] = useState<DesignScenarioRecipe | null>(null);
  const [projectId, setProjectId] = useState(initialContext?.projectId ?? "");
  const [agentId, setAgentId] = useState(initialContext?.agentId ?? "");
  const [repositoryId, setRepositoryId] = useState("");
  const [issueId, setIssueId] = useState(initialContext?.issueId ?? "");
  // Default on: a design run that leaves no trace on the tasks page is the
  // exception, not the norm. Turning it off is one click away.
  const [createIssue, setCreateIssue] = useState(!initialContext?.issueId);
  // Mutually exclusive by construction: the server refuses a request carrying
  // both, and the picker only ever sets one of them.
  const [designSystemId, setDesignSystemId] = useState("");
  const [builtinSlug, setBuiltinSlug] = useState("");
  const [platform, setPlatform] = useState<ProjectDesignSystemPlatform>("web");
  // Reference files staged with the prompt, as Open Design's composer does.
  // Uploaded through the ordinary route; only the ids travel with the request.
  const [attachments, setAttachments] = useState<Array<{ id: string; name: string }>>([]);
  // Focus only gates the placeholder animation (it freezes while the caret is
  // in the box); nothing else on the panel reads it.
  const [briefFocused, setBriefFocused] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // The empty design composer types rotating example briefs into the
  // placeholder; every other state shows the static line.
  const typedBriefPlaceholder = useTypewriterPlaceholder(PLACEHOLDER_BRIEF_EXAMPLES, {
    enabled: mode === "design" && brief.length === 0,
    paused: briefFocused,
  });
  const briefPlaceholder =
    mode === "design" && brief.length === 0 ? typedBriefPlaceholder : STATIC_BRIEF_PLACEHOLDER;
  const { upload, uploading } = useFileUpload(api, (error, file) => toast.error(`${file.name}：${error.message}`));
  const stageFiles = async (files: FileList | File[]) => {
    for (const file of Array.from(files).slice(0, MAX_ATTACHMENTS)) {
      try {
        const result = await upload(file);
        if (!result) continue;
        setAttachments((current) => (
          current.some((item) => item.id === result.id) || current.length >= MAX_ATTACHMENTS
            ? current
            : [...current, { id: result.id, name: result.filename || file.name }]
        ));
      } catch {
        // Reported through the hook's onError; nothing else to do here.
      }
    }
  };

  // Applying a catalogue recipe is an event, not derived state: it seeds the
  // brief once and then gets out of the way, so later edits to the words keep
  // the recipe the user chose. The gallery and the example rail hand over the
  // same shape, so they share this path.
  const applyRecipe = useCallback((picked: DesignScenarioRecipe) => {
    // A recipe is a design-mode artifact; applying one while asking or
    // planning is a mode switch, not a dead click.
    setMode("design");
    setRecipe(picked.slug);
    setAppliedRecipe(picked);
    setBrief(picked.prompt);
    setPlatform(picked.platform || "web");
  }, []);
  const appliedToken = useRef<number | null>(null);
  useEffect(() => {
    if (!recipeSelection || appliedToken.current === recipeSelection.token) return;
    appliedToken.current = recipeSelection.token;
    applyRecipe(recipeSelection.recipe);
  }, [applyRecipe, recipeSelection]);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projectResources = [], isSuccess: projectResourcesLoaded } = useQuery({
    ...projectResourcesOptions(wsId, projectId),
    enabled: !!projectId,
  });
  const { data: issues = [] } = useQuery(projectOpenIssuesOptions(wsId, projectId));
  const { data: contextIssue } = useQuery({
    ...issueDetailOptions(wsId, initialContext?.issueId ?? ""),
    enabled: !!initialContext?.issueId,
  });
  const { data: workspaceSystems = [] } = useQuery(projectDesignSystemCatalogueOptions(wsId));
  const { data: builtinSystems = [] } = useQuery(builtinDesignSystemListOptions(wsId));

  const selectedProject = projects.find((project) => project.id === projectId);
  const repositories = useMemo(
    () => projectResources.filter((resource) => resource.resource_type === "github_repo"),
    [projectResources],
  );
  const contextRepositorySeeded = useRef(false);
  useEffect(() => {
    if (!initialContext?.issueId || !projectResourcesLoaded || contextRepositorySeeded.current) return;
    contextRepositorySeeded.current = true;
    if (repositories.length === 1) setRepositoryId(repositories[0]!.id);
  }, [initialContext?.issueId, projectResourcesLoaded, repositories]);
  const issueOptions = contextIssue && !issues.some((issue) => issue.id === contextIssue.id)
    ? [contextIssue, ...issues]
    : issues;
  // A repository or issue chosen before the project changed no longer belongs
  // to it; the server would reject them, so drop them for rendering too.
  const activeRepositoryId = repositories.some((repository) => repository.id === repositoryId)
    ? repositoryId
    : "";
  const activeIssueId = initialContext?.issueId || (issues.some((issue) => issue.id === issueId) ? issueId : "");

  const trimmedBrief = brief.trim();
  const briefTooLong = brief.length > BRIEF_MAX_LENGTH;

  const createDocument = useMutation({
    mutationFn: () =>
      api.createDesignDocument({
        project_id: projectId,
        agent_id: agentId,
        ...(activeRepositoryId ? { project_resource_id: activeRepositoryId } : {}),
        ...(activeIssueId ? { issue_id: activeIssueId } : {}),
        ...(!activeIssueId && createIssue ? { create_issue: true } : {}),
        ...(designSystemId ? { design_system_id: designSystemId } : {}),
        ...(builtinSlug ? { builtin_design_system: builtinSlug } : {}),
        platform,
        recipe,
        brief: trimmedBrief,
        ...(attachments.length ? { attachments: attachments.map((item) => ({ attachment_id: item.id })) } : {}),
      }),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({
        queryKey: designKeys.documents(wsId, created.project_id || projectId),
      });
      const linkedIssueId = created.issue_id || activeIssueId;
      if (linkedIssueId) {
        await queryClient.invalidateQueries({
          queryKey: designKeys.documentsByIssue(wsId, linkedIssueId),
        });
      }
      // DC-053: never let the result read as if the agent inspected code when
      // it did not. The server's own flag decides, not what was submitted.
      toast.success(
        created.repository_grounded === true
          ? "已创建页面设计任务，智能体将对所选仓库做只读取证"
          : "已创建页面设计任务，本次未做仓库取证",
      );
      setBrief("");
      setAttachments([]);
      onCreated(created);
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "创建页面设计任务失败"),
  });

  // 规划 / 提问 hand the prompt to an agent chat: the session opens, the
  // first message is sent, and the composer navigates into the conversation.
  const startChat = useMutation({
    mutationFn: async () => {
      const session = await api.createChatSession({
        agent_id: agentId,
        title: trimmedBrief.slice(0, 60),
        ...(projectId ? { project_id: projectId } : {}),
      });
      const content = mode === "plan" ? planInstruction(trimmedBrief) : trimmedBrief;
      await api.sendChatMessage(session.id, content, attachments.map((item) => item.id));
      return session;
    },
    onSuccess: (session) => {
      setBrief("");
      setAttachments([]);
      navigation.push(paths.chatSession(session.id));
    },
    onError: (error) =>
      toast.error(error instanceof Error ? error.message : "发起对话失败"),
  });

  const missingRequirement = mode === "design" && !projectId
    ? "请选择项目"
    : !agentId
      ? "请选择智能体"
      : !trimmedBrief
        ? (mode === "ask" ? "请输入你的问题" : mode === "plan" ? "请描述要规划的内容" : "请描述你想要的页面")
        : briefTooLong
          ? `需求描述超出 ${BRIEF_MAX_LENGTH} 字上限`
          : uploading
            ? "参考文件上传中"
            : "";
  const submitPending = createDocument.isPending || startChat.isPending;
  const canSubmit = !missingRequirement && !submitPending;
  const activeMode = COMPOSER_MODES.find((item) => item.id === mode) ?? COMPOSER_MODES[1]!;
  const submitLabel = mode === "design" ? "生成页面设计" : mode === "plan" ? "生成规划" : "发送提问";

  return (
    <div className="relative min-h-0 flex-1 overflow-y-auto">
      <DesignDotGrid />
      {/* Open Design's own home margins: the page shell runs wide (their
          1600px) so the example wall and recent documents below can breathe,
          while the composer itself stays a narrower reading column (their
          960px) nested inside it — see the max-w-[960px] wrapper below. */}
      <div className="relative z-10 mx-auto flex w-full max-w-[1600px] flex-col px-4 py-8 sm:px-9 sm:py-10">
        <div className="mx-auto flex w-full max-w-[960px] flex-col">
          {/* The whole creation surface on one line. Only the scenarios with a
              real producer are live; the rest keep their position so the rail
              reads as complete without promising anything. */}
          <CreateTypeRail
            recipe={recipe}
            onPick={(picked) => {
              // A built-in chip and a catalogue recipe are the same field, so
              // picking one has to drop the other. Scenario chips are design
              // artifacts, so picking one in 规划/提问 switches back to 设计.
              setMode("design");
              setAppliedRecipe(null);
              setRecipe((current) => {
                const next = current === picked ? "default" : picked;
                // No platform pill on the composer (the preview page owns
                // device switching): the target platform follows the scenario.
                setPlatform(next === "mobile-app" ? "mobile" : "web");
                return next;
              });
            }}
          />

          {/* 来自 Figma is armed from the + menu, not the rail, so while it
              is the active recipe this line is its visible, clearable state —
              without it the armed migration would be invisible. */}
          {recipe === "figma-migration" ? (
            <div className="mt-2 flex items-center gap-2 self-start rounded-lg border bg-card px-2.5 py-1.5">
              <Frame className="size-3.5 shrink-0 text-muted-foreground" />
              <p className="min-w-0 flex-1 truncate text-caption">来自 Figma：把 Figma 稿转成页面设计</p>
              <button
                type="button"
                aria-label="取消从 Figma 导入"
                title="取消从 Figma 导入"
                className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                onClick={() => setRecipe("default")}
              >
                <X className="size-3" />
              </button>
            </div>
          ) : null}

          <div className="mt-3 rounded-2xl border bg-card shadow-sm transition-colors focus-within:border-primary/60">
            <Textarea
              value={brief}
              onChange={(event) => setBrief(event.target.value)}
              onFocus={() => setBriefFocused(true)}
              onBlur={() => setBriefFocused(false)}
              aria-label="页面需求描述"
              placeholder={briefPlaceholder}
              className="min-h-32 resize-none border-0 bg-transparent px-4 py-3.5 text-body shadow-none focus-visible:border-0 focus-visible:ring-0 dark:bg-transparent"
            />
            {attachments.length > 0 ? (
              <ul className="flex flex-wrap items-center gap-1.5 px-3 pb-2" aria-label="参考文件">
                {attachments.map((item) => (
                  <li key={item.id} className="inline-flex h-6 max-w-56 items-center gap-1 rounded-full border bg-background px-2 text-caption">
                    <Paperclip className="h-3 w-3 shrink-0 text-muted-foreground" />
                    <span className="truncate">{item.name}</span>
                    <button
                      type="button"
                      aria-label={`移除 ${item.name}`}
                      className="ml-0.5 rounded-full p-0.5 text-muted-foreground hover:text-foreground"
                      onClick={() => setAttachments((current) => current.filter((entry) => entry.id !== item.id))}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </li>
                ))}
              </ul>
            ) : null}
            {/* Settings live in the card so choosing a project never means
                leaving the sentence being written. */}
            <div className="flex flex-wrap items-center gap-x-2 gap-y-2 px-3 pb-3">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                accept="image/*,.pdf,.txt,.md,.json"
                className="hidden"
                aria-label="上传参考文件"
                onChange={(event) => {
                  if (event.target.files) void stageFiles(event.target.files);
                  event.target.value = "";
                }}
              />
              {/* Open Design's + menu: attach lives here rather than as its
                  own chip, alongside the shortcuts that have a real producer. */}
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      aria-label="添加"
                      title="添加"
                      className="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-full border bg-card text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    >
                      {uploading ? <LoaderCircle className="size-3.5 animate-spin" /> : <Plus className="size-4" />}
                    </button>
                  }
                />
                <DropdownMenuContent align="start" className="w-56">
                  <DropdownMenuItem
                    disabled={uploading || attachments.length >= MAX_ATTACHMENTS}
                    onClick={() => fileInputRef.current?.click()}
                  >
                    <Paperclip className="size-4" />
                    <span className="flex-1 truncate">附加文件</span>
                    {attachments.length > 0 ? (
                      <span className="shrink-0 text-caption text-muted-foreground">{attachments.length}/{MAX_ATTACHMENTS}</span>
                    ) : null}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      setMode("design");
                      setAppliedRecipe(null);
                      setRecipe("figma-migration");
                    }}
                  >
                    <Frame className="size-4" />
                    <span className="flex-1 truncate">从 Figma 导入</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              {mode === "design" ? (
                <>
                  <ProjectPicker
                    projectId={projectId || null}
                    onUpdate={(updates) => setProjectId(updates.project_id ?? "")}
                    align="start"
                    disabled={!!initialContext?.projectId}
                    triggerRender={
                      <SettingTrigger
                        filled={!!selectedProject}
                        disabled={!!initialContext?.projectId}
                        aria-label="项目"
                      />
                    }
                  />
                  <RepositorySetting
                    repositories={repositories}
                    repositoryId={activeRepositoryId}
                    disabled={!projectId}
                    onChange={setRepositoryId}
                  />
                  <IssueDestinationSetting
                    issues={issueOptions}
                    issueId={activeIssueId}
                    createIssue={createIssue}
                    disabled={!projectId || !!initialContext?.issueId}
                    onChangeIssue={setIssueId}
                    onChangeCreate={setCreateIssue}
                  />
                  <DesignSystemSetting
                    workspaceSystems={workspaceSystems}
                    builtinSystems={builtinSystems}
                    designSystemId={designSystemId}
                    builtinSlug={builtinSlug}
                    onChange={(selection) => {
                      setDesignSystemId(selection.designSystemId);
                      setBuiltinSlug(selection.builtinSlug);
                    }}
                  />
                </>
              ) : null}
              <div className="ml-auto flex min-w-0 items-center gap-2">
                {/* Open Design's mode chip: 规划 / 设计 / 提问, each with its
                    own submission path. */}
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={<SettingTrigger filled aria-label="创作模式" />}
                  >
                    <activeMode.icon className="size-3.5 shrink-0" />
                    <span className="truncate">{activeMode.label}</span>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-80">
                    {COMPOSER_MODES.map((item) => (
                      <DropdownMenuItem key={item.id} className="items-start gap-2 py-2" onClick={() => setMode(item.id)}>
                        <item.icon className="mt-0.5 size-4 shrink-0" />
                        <span className="min-w-0 flex-1">
                          <span className="flex items-center justify-between gap-2 text-body font-medium">
                            {item.label}
                            {item.id === mode ? <Check className="size-3.5 shrink-0" /> : null}
                          </span>
                          <span className="mt-0.5 block text-caption leading-5 text-muted-foreground">{item.description}</span>
                        </span>
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                <AgentSetting agents={agents} agentId={agentId} onChange={setAgentId} />
                {/* Open Design's round ↑ submit. The accessible name carries
                    what submission means in the current mode. */}
                <Button
                  type="button"
                  size="icon-sm"
                  aria-label={submitLabel}
                  title={submitLabel}
                  className="size-8 shrink-0 rounded-full"
                  disabled={!canSubmit}
                  onClick={() => (mode === "design" ? createDocument.mutate() : startChat.mutate())}
                >
                  {submitPending ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowUp className="size-4" />}
                </Button>
              </div>
            </div>
          </div>

          <div className="mt-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
            {/* Says which field is still missing instead of leaving a dead
                button — "disabled" on its own does not point anywhere. */}
            <p role="status" className="text-caption text-muted-foreground">
              {submitPending ? "" : missingRequirement}
            </p>
            <p
              className={cn(
                "ml-auto text-caption",
                briefTooLong ? "text-destructive" : "text-muted-foreground",
              )}
            >
              {brief.length} / {BRIEF_MAX_LENGTH}
            </p>
          </div>

          {/* A catalogue recipe is not one of the five chips, so without this row
              the user would have no sign of which scenario is armed. */}
          {appliedRecipe ? (
            <div className="mt-3 flex min-w-0 items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 px-3 py-2">
              <FileCode className="size-3.5 shrink-0 text-primary" />
              <p className="min-w-0 flex-1 truncate text-caption">
                <span className="text-muted-foreground">已套用社区配方：</span>
                <span className="font-medium text-foreground">
                  {appliedRecipe.title || appliedRecipe.slug}
                </span>
              </p>
              <button
                type="button"
                aria-label="不使用该社区配方"
                title="不使用该社区配方"
                className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                onClick={() => {
                  setAppliedRecipe(null);
                  setRecipe("default");
                }}
              >
                <X className="size-3" />
              </button>
            </div>
          ) : null}

          {/* The composer modes that hand the brief to a chat need one line
              saying where the submit goes; nothing else on the panel does. The
              repository / design-system state hint that used to share this slot
              was dropped at the user's request (2026-08-21): the "agent did not
              read code" guarantee lives in the post-submit toast and the
              server's repository_grounded flag, not in standing copy. */}
          {mode !== "design" ? (
            <p className="mt-3 text-caption text-muted-foreground">
              {mode === "plan"
                ? "规划会以对话进行：智能体产出可修改的设计规划，最终的需求描述可以带回这里生成设计稿。"
                : "提问会以对话进行，不创建新的设计稿。"}
            </p>
          ) : null}
        </div>

        {/* Open Design breaks its equivalent strip and grid out of the hero's
            narrow column to the full page shell — see the comment above. */}
        <DesignExamplePrompts
          onUse={applyRecipe}
          onBrowseRecipes={onBrowseRecipes}
          recipe={recipe}
          onPickPrototypeScene={(picked) => {
            setMode("design");
            setAppliedRecipe(null);
            setRecipe(picked);
            // Platform follows the scenario, same as the rail's own onPick.
            setPlatform(picked === "mobile-app" ? "mobile" : "web");
          }}
        />
        <DesignRecentDocuments onOpenDocument={onOpenDocument} />
      </div>
    </div>
  );
}
