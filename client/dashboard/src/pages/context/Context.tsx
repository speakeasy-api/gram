import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Icon, ResizablePanel } from "@speakeasy-api/moonshine";
import {
  BotIcon,
  ChevronDownIcon,
  GitCommitHorizontalIcon,
  MessageSquareIcon,
  MoveRightIcon,
  PlusIcon,
  SparklesIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
  Undo2Icon,
  UserIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Outlet } from "react-router";
import { AddRepoDialog } from "./AddRepoDialog";
import { AnnotationsPanel } from "./AnnotationsPanel";
import { FeedbackPanel } from "./FeedbackPanel";
import { ObservabilityTab } from "./ObservabilityTab";
import {
  type ContextFile,
  type ContextFolder,
  type ContextNode,
  type DiffLine,
  type DocsMcpConfig,
  type DraftDocument,
  type FileVersion,
  collectDrafts,
  computeLineDiff,
  countDrafts,
  countItems,
  findFile,
  formatDate,
  formatFileSize,
  formatRelativeTime,
  getEffectiveConfig,
  MOCK_ALL_ROLES,
  MOCK_CONTEXT_TREE,
  MOCK_DRAFT_DOCUMENTS,
  parseSkillFrontmatter,
  resolvePath,
} from "./mock-data";

export function ContextRoot() {
  return <Outlet />;
}

const RESIZABLE_PANEL_CLASS =
  "[&>[role='separator']]:w-px [&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:border-0 [&>[role='separator']]:hover:bg-primary [&>[role='separator']]:relative [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:cursor-col-resize";

export default function ContextPage() {
  const totalDrafts = MOCK_DRAFT_DOCUMENTS.filter(
    (d) => d.status === "open",
  ).length;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        <Tabs defaultValue="content" className="flex flex-col h-full">
          <div className="border-b">
            <div className="px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none items-stretch">
                <PageTabsTrigger value="content">Content</PageTabsTrigger>
                <PageTabsTrigger value="pending-changes">
                  Pending Changes
                  {totalDrafts > 0 && (
                    <span className="ml-1.5 inline-flex items-center justify-center h-5 min-w-5 px-1 rounded-full bg-amber-500 text-white text-xs font-medium leading-none">
                      {totalDrafts}
                    </span>
                  )}
                </PageTabsTrigger>
                <PageTabsTrigger value="observability">
                  Observability
                </PageTabsTrigger>
              </TabsList>
            </div>
          </div>
          <TabsContent value="content" className="flex-1 min-h-0">
            <ContentTab />
          </TabsContent>
          <TabsContent
            value="pending-changes"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <PendingChangesTab />
          </TabsContent>
          <TabsContent
            value="observability"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <ObservabilityTab />
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

// ── Content Tab ────────────────────────────────────────────────────────────

function ContentTab() {
  const [selectedFile, setSelectedFile] = useState<ContextFile | null>(null);
  const [selectedPath, setSelectedPath] = useState<string[]>([]);
  const [addRepoOpen, setAddRepoOpen] = useState(false);
  const [viewAsRole, setViewAsRole] = useState<string | null>(null);

  const effectiveConfig = getEffectiveConfig(MOCK_CONTEXT_TREE, selectedPath);
  const selectedFolder = resolvePath(MOCK_CONTEXT_TREE, selectedPath);

  const handleFileSelect = (file: ContextFile, path: string[]) => {
    setSelectedFile(file);
    setSelectedPath(path);
  };

  const handleFolderSelect = (path: string[]) => {
    setSelectedFile(null);
    setSelectedPath(path);
  };

  return (
    <>
      <ResizablePanel
        direction="horizontal"
        className={cn("h-full", RESIZABLE_PANEL_CLASS)}
      >
        {/* Left: tree explorer */}
        <ResizablePanel.Pane minSize={12} defaultSize={18} maxSize={30}>
          <div className="h-full flex flex-col overflow-hidden">
            <div className="px-3 py-2 border-b border-border space-y-1.5">
              <div className="flex items-center justify-between">
                <Type
                  small
                  muted
                  className="font-medium uppercase tracking-wider text-xs"
                >
                  Files
                </Type>
                <button
                  onClick={() => setAddRepoOpen(true)}
                  className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
                  title="Add content"
                >
                  <Icon name="plus" className="h-3.5 w-3.5" />
                </button>
              </div>
              {/* View-as-role selector */}
              <div className="flex items-center gap-1 flex-wrap">
                <button
                  onClick={() => setViewAsRole(null)}
                  className={cn(
                    "px-1.5 py-0.5 text-[10px] font-medium rounded transition-colors",
                    viewAsRole === null
                      ? "bg-foreground text-background"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
                  )}
                >
                  All
                </button>
                {MOCK_ALL_ROLES.map((role) => (
                  <button
                    key={role}
                    onClick={() => setViewAsRole(role)}
                    className={cn(
                      "px-1.5 py-0.5 text-[10px] font-medium rounded transition-colors",
                      viewAsRole === role
                        ? "bg-foreground text-background"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
                    )}
                  >
                    {role}
                  </button>
                ))}
              </div>
              {viewAsRole && (
                <div className="flex items-center gap-1 text-[10px] text-amber-600">
                  <Icon name="eye" className="h-3 w-3" />
                  <span>Viewing as {viewAsRole}</span>
                </div>
              )}
            </div>
            <div className="flex-1 overflow-y-auto py-1">
              <TreeNode
                node={MOCK_CONTEXT_TREE}
                depth={0}
                selectedFile={selectedFile}
                onFileSelect={handleFileSelect}
                onFolderSelect={handleFolderSelect}
                parentPath={[]}
                viewAsRole={viewAsRole}
              />
            </div>
          </div>
        </ResizablePanel.Pane>

        {/* Right: detail view */}
        <ResizablePanel.Pane minSize={40}>
          <div className="h-full overflow-y-auto p-6">
            {selectedFile ? (
              <FileDetail file={selectedFile} />
            ) : selectedFolder ? (
              <FolderDetail
                folder={selectedFolder}
                path={selectedPath}
                config={effectiveConfig}
              />
            ) : (
              <div className="h-full flex items-center justify-center">
                <div className="text-center space-y-2">
                  <Icon
                    name="file-text"
                    className="h-10 w-10 mx-auto text-muted-foreground/30"
                  />
                  <Type small muted>
                    Select a file or folder to view details
                  </Type>
                </div>
              </div>
            )}
          </div>
        </ResizablePanel.Pane>
      </ResizablePanel>

      <AddRepoDialog
        open={addRepoOpen}
        onOpenChange={setAddRepoOpen}
        onComplete={() => {}}
      />
    </>
  );
}

// ── Tree View ─────────────────────────────────────────────────────────────

/** Collect all .docs-mcp.json configs from root and nested folders. */
function collectConfigs(folder: ContextFolder): DocsMcpConfig[] {
  const configs: DocsMcpConfig[] = [];
  for (const child of folder.children) {
    if (
      child.type === "file" &&
      child.kind === "mcp-docs-config" &&
      child.config
    ) {
      configs.push(child.config);
    }
    if (child.type === "folder") {
      configs.push(...collectConfigs(child));
    }
  }
  return configs;
}

const ALL_CONFIGS = collectConfigs(MOCK_CONTEXT_TREE);

/** Check if a path is denied for a role across all .docs-mcp.json configs. */
function isPathDeniedForRole(role: string, nodePath: string): boolean {
  return ALL_CONFIGS.some((config) => {
    if (!config.accessControl) return false;
    const rule = config.accessControl.find((r) => r.role === role);
    if (!rule) return false;
    return (rule.deniedPaths ?? []).some((pattern) => {
      const prefix = pattern.replace(/\*$/, "");
      return nodePath.startsWith(prefix) || nodePath === pattern;
    });
  });
}

function TreeNode({
  node,
  depth,
  selectedFile,
  onFileSelect,
  onFolderSelect,
  parentPath,
  viewAsRole,
}: {
  node: ContextNode;
  depth: number;
  selectedFile: ContextFile | null;
  onFileSelect: (file: ContextFile, path: string[]) => void;
  onFolderSelect: (path: string[]) => void;
  parentPath: string[];
  viewAsRole?: string | null;
}) {
  const [expanded, setExpanded] = useState(depth < 1);

  // Build the path string for access control checks
  const nodePath =
    depth === 0
      ? ""
      : [...parentPath, node.name].join("/") +
        (node.type === "folder" ? "/" : "");
  const isDenied =
    viewAsRole != null && nodePath && isPathDeniedForRole(viewAsRole, nodePath);

  // Files indent past the chevron column so they align with the folder name,
  // not the chevron. Folders: base + chevron(w-3) + gap(1.5) + folder-icon.
  // Files: base + spacer matching chevron+gap + file-icon.
  const INDENT_PX = 14;
  const CHEVRON_SPACER = 18; // 12px chevron + 6px gap

  if (node.type === "file") {
    const isSelected = selectedFile?.name === node.name;
    return (
      <button
        onClick={() => !isDenied && onFileSelect(node, parentPath)}
        className={cn(
          "flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm",
          isDenied && "opacity-30 cursor-not-allowed",
          !isDenied && isSelected
            ? "bg-primary/10 text-foreground"
            : !isDenied &&
                "text-muted-foreground hover:text-foreground hover:bg-muted/50",
        )}
        style={{ paddingLeft: depth * INDENT_PX + 8 + CHEVRON_SPACER }}
      >
        <Icon name={getFileIcon(node)} className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{node.name}</span>
        {isDenied && (
          <Icon
            name="lock"
            className="ml-auto h-3 w-3 shrink-0 text-destructive/50"
          />
        )}
        {!isDenied && node.draft && (
          <span className="ml-auto h-1.5 w-1.5 rounded-full bg-amber-500 shrink-0" />
        )}
      </button>
    );
  }

  // Folder
  const folderPath = depth === 0 ? [] : [...parentPath, node.name];
  const draftCount = countDrafts(node);

  const sortedChildren = [...node.children].sort((a, b) => {
    if (a.type !== b.type) return a.type === "folder" ? -1 : 1;
    if (a.type === "file" && b.type === "file") {
      if (a.kind === "mcp-docs-config") return -1;
      if (b.kind === "mcp-docs-config") return 1;
    }
    return a.name.localeCompare(b.name);
  });

  return (
    <div>
      <button
        onClick={() => {
          setExpanded((v) => !v);
          if (!isDenied) onFolderSelect(folderPath);
        }}
        className={cn(
          "flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm",
          isDenied && "opacity-30",
          !isDenied &&
            "text-muted-foreground hover:text-foreground hover:bg-muted/50",
        )}
        style={{ paddingLeft: depth * INDENT_PX + 8 }}
      >
        <Icon
          name={expanded ? "chevron-down" : "chevron-right"}
          className="h-3 w-3 shrink-0"
        />
        <Icon name="folder" className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-medium">
          {depth === 0 ? "docs" : node.name}
        </span>
        {isDenied && (
          <Icon
            name="lock"
            className="ml-auto h-3 w-3 shrink-0 text-destructive/50"
          />
        )}
        {!isDenied && draftCount > 0 && (
          <span className="ml-auto h-1.5 w-1.5 rounded-full bg-amber-500 shrink-0" />
        )}
      </button>
      {expanded && (
        <div>
          {sortedChildren.map((child) => (
            <TreeNode
              key={child.name}
              node={child}
              depth={depth + 1}
              selectedFile={selectedFile}
              onFileSelect={onFileSelect}
              onFolderSelect={onFolderSelect}
              parentPath={folderPath}
              viewAsRole={viewAsRole}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ── Draft Documents Tab (Reddit-style) ────────────────────────────────────

type YoloSchedule = "off" | "24h" | "weekly";

function PendingChangesTab() {
  const drafts = MOCK_DRAFT_DOCUMENTS;
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [sortBy, setSortBy] = useState<"hot" | "new" | "top">("hot");
  const [yoloSchedule, setYoloSchedule] = useState<YoloSchedule>("off");

  const sorted = useMemo(() => {
    const open = drafts.filter((d) => d.status === "open");
    switch (sortBy) {
      case "new":
        return [...open].sort(
          (a, b) =>
            new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
        );
      case "top":
        return [...open].sort(
          (a, b) => b.upvotes - b.downvotes - (a.upvotes - a.downvotes),
        );
      default:
        return [...open].sort(
          (a, b) =>
            b.upvotes -
            b.downvotes +
            b.comments.length * 2 -
            (a.upvotes - a.downvotes + a.comments.length * 2),
        );
    }
  }, [drafts, sortBy]);

  if (sorted.length === 0) {
    return (
      <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[300px]">
        <div className="text-center space-y-2">
          <Icon
            name="check-circle"
            className="h-10 w-10 text-muted-foreground/50 mx-auto"
          />
          <Type variant="subheading" className="text-muted-foreground">
            No draft documents
          </Type>
          <Type small muted>
            All content is published and up to date.
          </Type>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-4xl space-y-3">
      {/* Sort bar + yolo mode */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-1 rounded-lg bg-muted/50 p-1">
          {(["hot", "new", "top"] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSortBy(s)}
              className={cn(
                "px-3 py-1 text-xs font-medium rounded-md transition-colors capitalize",
                sortBy === s
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {s}
            </button>
          ))}
        </div>
        <Type small muted>
          {sorted.length} open draft{sorted.length !== 1 && "s"}
        </Type>
        <div className="ml-auto">
          <YoloModeToggle schedule={yoloSchedule} onChange={setYoloSchedule} />
        </div>
      </div>

      {/* Yolo mode banner */}
      {yoloSchedule !== "off" && (
        <div className="flex items-center gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-3">
          <SparklesIcon className="h-4 w-4 text-amber-500 shrink-0" />
          <div className="flex-1">
            <Type small className="font-medium text-foreground">
              Auto-iterate is active
            </Type>
            <Type small muted>
              Every {yoloSchedule === "24h" ? "24 hours" : "week"}, an agent
              will incorporate comments and publish approved drafts
              automatically.
            </Type>
          </div>
          <button
            onClick={() => setYoloSchedule("off")}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors shrink-0"
          >
            Disable
          </button>
        </div>
      )}

      {/* Draft list */}
      <div className="space-y-2">
        {sorted.map((draft) => (
          <DraftDocumentCard
            key={draft.id}
            draft={draft}
            expanded={expandedId === draft.id}
            onToggle={() =>
              setExpandedId(expandedId === draft.id ? null : draft.id)
            }
          />
        ))}
      </div>
    </div>
  );
}

function YoloModeToggle({
  schedule,
  onChange,
}: {
  schedule: YoloSchedule;
  onChange: (s: YoloSchedule) => void;
}) {
  const [open, setOpen] = useState(false);

  if (schedule !== "off") {
    return (
      <div className="flex items-center gap-2">
        <span className="text-xs text-amber-600 font-medium flex items-center gap-1">
          <SparklesIcon className="h-3 w-3" />
          YOLO: {schedule === "24h" ? "Daily" : "Weekly"}
        </span>
        <button
          onClick={() => onChange("off")}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          Off
        </button>
      </div>
    );
  }

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        <SparklesIcon className="h-3.5 w-3.5" />
        YOLO Mode
      </button>
    );
  }

  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-card p-1">
      <Type small muted className="px-2 text-xs">
        Auto-iterate & publish:
      </Type>
      <button
        onClick={() => {
          onChange("24h");
          setOpen(false);
        }}
        className="px-2.5 py-1 text-xs rounded-md hover:bg-muted/50 text-foreground transition-colors"
      >
        Every 24h
      </button>
      <button
        onClick={() => {
          onChange("weekly");
          setOpen(false);
        }}
        className="px-2.5 py-1 text-xs rounded-md hover:bg-muted/50 text-foreground transition-colors"
      >
        Weekly
      </button>
      <button
        onClick={() => setOpen(false)}
        className="px-2 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        Cancel
      </button>
    </div>
  );
}

function DraftDocumentCard({
  draft,
  expanded,
  onToggle,
}: {
  draft: DraftDocument;
  expanded: boolean;
  onToggle: () => void;
}) {
  const score = draft.upvotes - draft.downvotes;
  const isEdit = draft.filePath !== null;
  const [iterateState, setIterateState] = useState<IterateState>("idle");
  const [iteratePrompt, setIteratePrompt] = useState("");
  const [showPrompt, setShowPrompt] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(0 as never);

  const handleIterate = useCallback(() => {
    if (iterateState === "processing") return;
    setIterateState("processing");
    setShowPrompt(false);
    timerRef.current = setTimeout(() => setIterateState("done"), 3000);
  }, [iterateState]);

  const handleUndo = useCallback(() => {
    clearTimeout(timerRef.current);
    setIterateState("idle");
    setIteratePrompt("");
  }, []);

  useEffect(() => () => clearTimeout(timerRef.current), []);

  const isProcessing = iterateState === "processing";
  const isDone = iterateState === "done";

  return (
    <div
      className={cn(
        "rounded-lg border border-border bg-card overflow-hidden transition-all duration-500",
        isProcessing && "opacity-40 grayscale",
      )}
    >
      <div className="flex">
        {/* Vote column */}
        <div className="flex flex-col items-center gap-0.5 px-3 py-3 bg-muted/20 border-r border-border">
          <button
            className={cn(
              "p-0.5 rounded transition-colors",
              draft.userVote === "up"
                ? "text-primary"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <ThumbsUpIcon className="h-4 w-4" />
          </button>
          <span
            className={cn(
              "text-sm font-bold tabular-nums",
              score > 0
                ? "text-primary"
                : score < 0
                  ? "text-destructive"
                  : "text-muted-foreground",
            )}
          >
            {score}
          </span>
          <button
            className={cn(
              "p-0.5 rounded transition-colors",
              draft.userVote === "down"
                ? "text-destructive"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <ThumbsDownIcon className="h-4 w-4" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0 p-3">
          {/* Header */}
          <div className="flex items-start gap-2 mb-1">
            <button onClick={onToggle} className="flex-1 text-left min-w-0">
              <Type
                variant="subheading"
                className="hover:text-primary transition-colors"
              >
                {draft.title}
              </Type>
            </button>
            <div className="flex gap-1 shrink-0">
              <Button size="sm" variant="outline" className="h-6 px-2 text-xs">
                Publish
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs text-destructive"
              >
                Reject
              </Button>
            </div>
          </div>

          {/* Meta line */}
          <div className="flex items-center gap-2 flex-wrap text-xs text-muted-foreground mb-2">
            <span>
              submitted {formatRelativeTime(draft.createdAt)} by{" "}
              <span className="font-medium text-foreground">
                {draft.author}
              </span>
            </span>
            {draft.authorType === "agent" && (
              <Badge variant="default" className="text-[10px] px-1.5 py-0">
                Agent
              </Badge>
            )}
            {isEdit ? (
              <span className="font-mono text-xs">{draft.filePath}</span>
            ) : (
              <>
                <Badge
                  variant="outline"
                  className="border-emerald-500/50 text-emerald-600 bg-emerald-500/10 text-[10px] px-1.5 py-0"
                >
                  New Doc
                </Badge>
                {draft.proposedPath && (
                  <span className="font-mono text-xs">
                    → {draft.proposedPath}
                  </span>
                )}
              </>
            )}
          </div>

          {/* Labels */}
          <div className="flex items-center gap-1.5 flex-wrap mb-2">
            {draft.labels.map((label) => (
              <Badge key={label} variant="secondary" className="text-[10px]">
                {label}
              </Badge>
            ))}
          </div>

          {/* Action bar */}
          <div className="flex items-center gap-4 text-xs text-muted-foreground">
            <button
              onClick={onToggle}
              className="flex items-center gap-1 hover:text-foreground transition-colors"
            >
              <MessageSquareIcon className="h-3.5 w-3.5" />
              {draft.comments.length} comment
              {draft.comments.length !== 1 && "s"}
            </button>
            <button
              onClick={onToggle}
              className="flex items-center gap-1 hover:text-foreground transition-colors"
            >
              <Icon name="git-compare" className="h-3.5 w-3.5" />
              {isEdit ? "View diff" : "Preview"}
            </button>
          </div>
        </div>
      </div>

      {/* Expanded: diff + comments */}
      {expanded && (
        <div className="border-t border-border">
          {/* Diff / preview */}
          <div className="p-4 border-b border-border">
            {isEdit && draft.originalContent ? (
              <DiffView
                oldText={draft.originalContent}
                newText={draft.content}
              />
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 max-h-[400px] overflow-auto">
                {draft.content}
              </pre>
            )}
          </div>

          {/* Done banner */}
          {isDone && (
            <div className="flex items-center justify-between px-5 py-3 bg-emerald-500/10 border-b border-emerald-500/30">
              <div className="flex items-center gap-2 text-sm text-emerald-600">
                <SparklesIcon className="h-4 w-4" />
                <span className="font-medium">
                  Agent incorporated {draft.comments.length} comment
                  {draft.comments.length !== 1 && "s"} into a new draft
                </span>
              </div>
              <button
                onClick={handleUndo}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                <Undo2Icon className="h-3.5 w-3.5" />
                Undo
              </button>
            </div>
          )}

          {/* Comments */}
          {!isDone && (
            <div className="p-5 space-y-4">
              {draft.comments.map((comment) => (
                <DraftCommentItem key={comment.id} comment={comment} />
              ))}

              {/* Iterate action */}
              {draft.comments.length > 0 && (
                <div className="pt-2 border-t border-border">
                  {showPrompt ? (
                    <div className="flex items-center gap-2">
                      <input
                        type="text"
                        value={iteratePrompt}
                        onChange={(e) => setIteratePrompt(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && handleIterate()}
                        placeholder="Incorporate all comments into the doc..."
                        className="flex-1 h-8 px-3 text-sm rounded-md border border-border bg-transparent focus:outline-none focus:border-ring"
                        autoFocus
                      />
                      <Button
                        size="sm"
                        className="h-8 px-4 text-xs gap-1.5"
                        onClick={handleIterate}
                      >
                        <SparklesIcon className="h-3.5 w-3.5" />
                        Iterate
                      </Button>
                      <button
                        onClick={() => {
                          setShowPrompt(false);
                          setIteratePrompt("");
                        }}
                        className="text-xs text-muted-foreground hover:text-foreground transition-colors"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setShowPrompt(true)}
                      className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
                    >
                      <SparklesIcon className="h-4 w-4" />
                      Iterate on {draft.comments.length} comment
                      {draft.comments.length !== 1 && "s"}
                    </button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function DraftCommentItem({
  comment,
}: {
  comment: DraftDocument["comments"][number];
}) {
  return (
    <div className="flex gap-3">
      {/* Vote */}
      <div className="flex flex-col items-center gap-0.5 pt-1">
        <button className="text-muted-foreground hover:text-foreground transition-colors">
          <ThumbsUpIcon className="h-3 w-3" />
        </button>
        <span className="text-[10px] font-medium text-muted-foreground tabular-nums">
          {comment.upvotes}
        </span>
      </div>
      {/* Body */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5 text-xs mb-0.5">
          <span className="font-medium text-foreground">{comment.author}</span>
          {comment.authorType === "agent" && (
            <Badge variant="default" className="text-[10px] px-1 py-0">
              Agent
            </Badge>
          )}
          <span className="text-muted-foreground">
            {formatRelativeTime(comment.createdAt)}
          </span>
        </div>
        <Type small className="text-foreground">
          {comment.content}
        </Type>
      </div>
    </div>
  );
}

// ── Observability Tab ─────────────────────────────────────────────────────
// Extracted to ./ObservabilityTab.tsx — uses API hooks instead of mock data.

// ── Shared SkillPreview ───────────────────────────────────────────────────

function SkillPreview({
  content,
  compact = false,
}: {
  content: string;
  compact?: boolean;
}) {
  const { meta, body } = parseSkillFrontmatter(content);

  const metaEntries = Object.entries(meta).filter(
    ([key]) => key !== "name" && key !== "description",
  );

  return (
    <div className={cn("flex flex-col", compact ? "gap-2" : "gap-3")}>
      {meta.name && <Type variant="subheading">{meta.name}</Type>}

      {meta.description && (
        <Type small muted>
          {meta.description}
        </Type>
      )}

      {metaEntries.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {metaEntries.map(([key, value]) => (
            <Badge key={key} variant="secondary">
              {key}: {value}
            </Badge>
          ))}
        </div>
      )}

      {body && (
        <pre
          className={cn(
            "overflow-auto rounded-md bg-muted/30 p-3 font-mono text-xs leading-relaxed text-foreground",
            compact ? "max-h-48" : "max-h-64",
          )}
        >
          {body}
        </pre>
      )}
    </div>
  );
}

// ── Sub-components ─────────────────────────────────────────────────────────

function DraftBadge() {
  return (
    <Badge
      variant="outline"
      className="border-amber-500/50 text-amber-600 bg-amber-500/10"
    >
      Draft
    </Badge>
  );
}

function FileKindBadge({ kind }: { kind: ContextFile["kind"] }) {
  switch (kind) {
    case "skill":
      return <Badge variant="default">Skill</Badge>;
    case "mcp-docs-config":
      return <Badge variant="secondary">Config</Badge>;
    default:
      return null;
  }
}

function FileDetail({ file }: { file: ContextFile }) {
  const [viewMode, setViewMode] = useState<
    "published" | "draft" | "diff" | "history"
  >(file.draft ? "diff" : "published");

  const displayContent =
    viewMode === "draft" && file.draft?.content
      ? file.draft.content
      : file.content;

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <Icon
            name={getFileIcon(file)}
            className="h-4 w-4 text-muted-foreground"
          />
          <Type variant="subheading" className="truncate">
            {file.name}
          </Type>
          {file.draft && <DraftBadge />}
        </div>
        <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
          <span>{formatFileSize(file.size)}</span>
          <span>Updated {formatDate(file.updatedAt)}</span>
          {file.source === "github" && (
            <button className="flex items-center gap-1 hover:text-foreground transition-colors ml-auto">
              <Icon name="github" className="h-3 w-3" />
              Open in GitHub
            </button>
          )}
        </div>
        <div className="flex items-center gap-1 mt-2">
          <LayerToggle
            active={viewMode === "published"}
            onClick={() => setViewMode("published")}
          >
            Published
          </LayerToggle>
          {file.draft && (
            <>
              <LayerToggle
                active={viewMode === "draft"}
                onClick={() => setViewMode("draft")}
              >
                Draft
              </LayerToggle>
              <LayerToggle
                active={viewMode === "diff"}
                onClick={() => setViewMode("diff")}
              >
                Diff
              </LayerToggle>
            </>
          )}
          <LayerToggle
            active={viewMode === "history"}
            onClick={() => setViewMode("history")}
          >
            {file.versions.length} Version
            {file.versions.length !== 1 && "s"}
          </LayerToggle>
        </div>
      </div>

      {viewMode === "history" && <VersionHistory file={file} />}

      {viewMode === "diff" && file.draft?.content && file.content && (
        <div className="p-4">
          <DiffView oldText={file.content} newText={file.draft.content} />
        </div>
      )}

      {viewMode !== "diff" &&
        viewMode !== "history" &&
        file.kind === "mcp-docs-config" &&
        file.config && <ConfigDetail config={file.config} />}

      {viewMode !== "diff" &&
        viewMode !== "history" &&
        file.kind === "skill" &&
        displayContent && (
          <div className="p-4">
            <SkillPreview content={displayContent} compact />
          </div>
        )}

      {viewMode !== "diff" &&
        viewMode !== "history" &&
        file.kind === "markdown" &&
        displayContent && (
          <div className="p-4">
            <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 overflow-auto max-h-[500px]">
              {displayContent}
            </pre>
          </div>
        )}

      <FeedbackPanel filePath={file.name} />
      <AnnotationsPanel filePath={file.name} />

      <div className="px-4 py-3 border-t border-border flex gap-2">
        {file.draft ? (
          <>
            <Button size="sm" className="flex-1">
              Publish Draft
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="flex-1 text-destructive"
            >
              Discard
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" variant="outline" className="flex-1">
              <Icon name="pencil" className="h-3.5 w-3.5 mr-1.5" />
              Edit
            </Button>
            <Button size="sm" variant="outline" className="flex-1">
              <Icon name="download" className="h-3.5 w-3.5 mr-1.5" />
              Download
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

// ── Source Badge ───────────────────────────────────────────────────────────

function LayerToggle({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 text-xs rounded-md transition-colors",
        active
          ? "bg-foreground text-background"
          : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
      )}
    >
      {children}
    </button>
  );
}

// ── Diff View ─────────────────────────────────────────────────────────────

function DiffView({ oldText, newText }: { oldText: string; newText: string }) {
  const lines = useMemo(
    () => computeLineDiff(oldText, newText),
    [oldText, newText],
  );

  return (
    <div className="rounded-md border border-border overflow-auto max-h-[500px] text-xs font-mono">
      {lines.map((line, i) => (
        <DiffLineRow key={i} line={line} />
      ))}
    </div>
  );
}

function DiffLineRow({ line }: { line: DiffLine }) {
  const bgClass = {
    same: "",
    added: "bg-emerald-500/10",
    removed: "bg-destructive/10",
  }[line.type];

  const textClass = {
    same: "text-foreground",
    added: "text-emerald-600",
    removed: "text-destructive",
  }[line.type];

  const prefix = { same: " ", added: "+", removed: "-" }[line.type];

  return (
    <div className={cn("flex", bgClass)}>
      <span
        className={cn(
          "w-5 shrink-0 text-right pr-1 select-none",
          line.type === "same" ? "text-muted-foreground/50" : textClass,
        )}
      >
        {prefix}
      </span>
      <span className={cn("flex-1 px-2 py-px whitespace-pre-wrap", textClass)}>
        {line.content || "\u00A0"}
      </span>
    </div>
  );
}

function VersionHistory({ file }: { file: ContextFile }) {
  const [selectedVersion, setSelectedVersion] = useState<FileVersion | null>(
    null,
  );
  const [viewMode, setViewMode] = useState<"content" | "diff">("content");

  const previousVersion = useMemo(() => {
    if (!selectedVersion) return null;
    return (
      file.versions.find((v) => v.version === selectedVersion.version - 1) ??
      null
    );
  }, [selectedVersion, file.versions]);

  const latestVersion = file.versions[0];

  return (
    <div className="border-t border-border">
      {/* Version list */}
      <div className="max-h-[240px] overflow-y-auto">
        {file.versions.map((v) => {
          const isSelected = selectedVersion?.version === v.version;
          const prevV = file.versions.find(
            (pv) => pv.version === v.version - 1,
          );
          const wasRenamed = prevV?.path && v.path && prevV.path !== v.path;

          return (
            <button
              type="button"
              key={v.version}
              onClick={() => setSelectedVersion(isSelected ? null : v)}
              className={cn(
                "w-full flex items-start gap-3 px-4 py-2.5 text-xs border-b border-border last:border-b-0 transition-colors text-left",
                isSelected
                  ? "bg-primary/5 border-l-2 border-l-primary"
                  : "hover:bg-muted/30",
              )}
            >
              {/* Version indicator */}
              <div className="flex flex-col items-center pt-0.5 shrink-0">
                <GitCommitHorizontalIcon className="h-3.5 w-3.5 text-muted-foreground" />
              </div>

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="font-mono text-muted-foreground">
                    v{v.version}
                  </span>
                  {v.version === latestVersion?.version && (
                    <Badge
                      variant="secondary"
                      className="text-[10px] px-1 py-0 h-4"
                    >
                      latest
                    </Badge>
                  )}
                  {v.agent && (
                    <span
                      className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground bg-muted/50 rounded px-1 py-0"
                      title={`Generated by ${v.agent}`}
                    >
                      <BotIcon className="h-2.5 w-2.5" />
                      {v.agent}
                    </span>
                  )}
                </div>

                <span className="text-foreground truncate block mt-0.5">
                  {v.message}
                </span>

                {/* Author / Committer */}
                <div className="flex items-center gap-2 mt-1 text-muted-foreground">
                  <span
                    className="inline-flex items-center gap-0.5"
                    title="Author"
                  >
                    <UserIcon className="h-2.5 w-2.5" />
                    {v.author}
                  </span>
                  {v.committer && v.committer !== v.author && (
                    <span
                      className="inline-flex items-center gap-0.5"
                      title="Committer"
                    >
                      <GitCommitHorizontalIcon className="h-2.5 w-2.5" />
                      {v.committer}
                    </span>
                  )}
                  <span>&middot;</span>
                  <span>{formatDate(v.updatedAt)}</span>
                  <span>&middot;</span>
                  <span>{formatFileSize(v.size)}</span>
                </div>

                {/* Rename indicator */}
                {wasRenamed && (
                  <div className="flex items-center gap-1 mt-1 text-amber-600 dark:text-amber-400">
                    <MoveRightIcon className="h-2.5 w-2.5" />
                    <span className="font-mono truncate">{prevV.path}</span>
                    <MoveRightIcon className="h-2.5 w-2.5" />
                    <span className="font-mono truncate">{v.path}</span>
                  </div>
                )}
              </div>

              {v.version !== latestVersion?.version && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-xs shrink-0"
                  onClick={(e) => e.stopPropagation()}
                >
                  Restore
                </Button>
              )}
            </button>
          );
        })}
      </div>

      {/* Selected version detail */}
      {selectedVersion && selectedVersion.content && (
        <div className="border-t border-border">
          <div className="flex items-center gap-1 px-4 py-2 border-b border-border bg-muted/20">
            <span className="text-xs font-medium text-muted-foreground mr-2">
              v{selectedVersion.version}
            </span>
            <LayerToggle
              active={viewMode === "content"}
              onClick={() => setViewMode("content")}
            >
              Content
            </LayerToggle>
            {previousVersion?.content && (
              <LayerToggle
                active={viewMode === "diff"}
                onClick={() => setViewMode("diff")}
              >
                Diff from v{previousVersion.version}
              </LayerToggle>
            )}
          </div>
          <div className="p-4">
            {viewMode === "diff" && previousVersion?.content ? (
              <DiffView
                oldText={previousVersion.content}
                newText={selectedVersion.content}
              />
            ) : (
              <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 overflow-auto max-h-[400px]">
                {selectedVersion.content}
              </pre>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function ConfigDetail({ config }: { config: DocsMcpConfig }) {
  return (
    <div className="p-4 space-y-4">
      {config.strategy && (
        <ConfigSection title="Chunking Strategy">
          <div className="grid grid-cols-2 gap-2 text-xs">
            <ConfigField label="Chunk by" value={config.strategy.chunk_by} />
            {config.strategy.max_chunk_size && (
              <ConfigField
                label="Max size"
                value={`${config.strategy.max_chunk_size.toLocaleString()} chars`}
              />
            )}
            {config.strategy.min_chunk_size && (
              <ConfigField
                label="Min size"
                value={`${config.strategy.min_chunk_size.toLocaleString()} chars`}
              />
            )}
          </div>
        </ConfigSection>
      )}

      {config.metadata && Object.keys(config.metadata).length > 0 && (
        <ConfigSection title="Metadata">
          <div className="flex flex-wrap gap-1.5">
            {Object.entries(config.metadata).map(([key, value]) => (
              <Badge key={key} variant="secondary">
                {key}: {value}
              </Badge>
            ))}
          </div>
        </ConfigSection>
      )}

      {config.taxonomy && Object.keys(config.taxonomy).length > 0 && (
        <ConfigSection title="Taxonomy">
          {Object.entries(config.taxonomy).map(([field, cfg]) => (
            <div key={field} className="mb-2 last:mb-0">
              <div className="flex items-center gap-2 text-xs mb-1">
                <span className="font-medium text-foreground">{field}</span>
                {cfg.vector_collapse && (
                  <Badge variant="outline">vector collapse</Badge>
                )}
              </div>
              {cfg.properties && (
                <div className="flex flex-wrap gap-1 ml-3">
                  {Object.entries(cfg.properties).map(([val, props]) => (
                    <Badge
                      key={val}
                      variant={props.mcp_resource ? "default" : "secondary"}
                    >
                      {val}
                      {props.mcp_resource && " (resource)"}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          ))}
        </ConfigSection>
      )}

      {config.mcpServerInstructions && (
        <ConfigSection title="Server Instructions">
          <p className="text-xs text-muted-foreground leading-relaxed">
            {config.mcpServerInstructions}
          </p>
        </ConfigSection>
      )}

      {config.overrides && config.overrides.length > 0 && (
        <ConfigSection title="Overrides">
          {config.overrides.map((override, i) => (
            <div
              key={i}
              className="text-xs bg-muted/30 rounded-md p-2 mb-1.5 last:mb-0"
            >
              <code className="font-mono text-foreground">
                {override.pattern}
              </code>
              {override.strategy && (
                <span className="text-muted-foreground ml-2">
                  chunk_by: {override.strategy.chunk_by}
                </span>
              )}
              {override.metadata && (
                <div className="flex gap-1 mt-1">
                  {Object.entries(override.metadata).map(([k, v]) => (
                    <Badge key={k} variant="secondary">
                      {k}: {v}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          ))}
        </ConfigSection>
      )}

      {config.accessControl && config.accessControl.length > 0 && (
        <AccessControlSection
          initialRules={config.accessControl}
          taxonomy={config.taxonomy}
        />
      )}
    </div>
  );
}

// All known taxonomy values and folder paths for the dropdown options.
const ALL_TAXONOMY_VALUES: Record<string, string[]> = {
  department: ["engineering", "sales", "finance"],
  audience: ["internal", "external", "partner"],
};
const ALL_FOLDER_PATHS = [
  "product/*",
  "engineering/*",
  "engineering/onboarding/*",
  "engineering/runbooks/*",
  "sales/*",
  "sales/competitive-intel/*",
  "finance/*",
  "finance/reporting/*",
  "company/*",
];

type AccessRule = NonNullable<DocsMcpConfig["accessControl"]>[number];
type AccessDefault = "allow-all" | "deny-all";
type RoleAccessOverride = "default" | "custom" | "deny";

function AccessControlSection({
  initialRules,
  taxonomy,
}: {
  initialRules: AccessRule[];
  taxonomy?: DocsMcpConfig["taxonomy"];
}) {
  const [defaultPolicy, setDefaultPolicy] =
    useState<AccessDefault>("allow-all");
  const [rules, setRules] = useState<AccessRule[]>(initialRules);
  const [roleOverrides, setRoleOverrides] = useState<
    Record<string, RoleAccessOverride>
  >(() => {
    const result: Record<string, RoleAccessOverride> = {};
    for (const role of MOCK_ALL_ROLES) {
      const rule = initialRules.find((r) => r.role === role);
      if (rule) result[role] = "custom";
      else result[role] = "default";
    }
    return result;
  });
  const [expandedRole, setExpandedRole] = useState<string | null>(null);

  const taxonomyFields = taxonomy
    ? Object.keys(taxonomy)
    : Object.keys(ALL_TAXONOMY_VALUES);

  const getTaxonomyValues = (field: string) =>
    taxonomy?.[field]?.properties
      ? Object.keys(taxonomy[field].properties!)
      : (ALL_TAXONOMY_VALUES[field] ?? []);

  const cycleOverride = (role: string) => {
    const order: RoleAccessOverride[] = ["default", "custom", "deny"];
    const current = roleOverrides[role] ?? "default";
    const next = order[(order.indexOf(current) + 1) % order.length];
    setRoleOverrides((prev) => ({ ...prev, [role]: next }));
    // When switching to custom, ensure a rule exists
    if (next === "custom" && !rules.find((r) => r.role === role)) {
      setRules((prev) => [
        ...prev,
        { role, allowedTaxonomy: {}, deniedPaths: [] },
      ]);
      setExpandedRole(role);
    }
    if (next !== "custom") setExpandedRole(null);
  };

  const toggleTaxonomyValue = (role: string, field: string, value: string) => {
    setRules((prev) =>
      prev.map((r) => {
        if (r.role !== role) return r;
        const current = r.allowedTaxonomy?.[field] ?? [];
        const next = current.includes(value)
          ? current.filter((v) => v !== value)
          : [...current, value];
        return {
          ...r,
          allowedTaxonomy: { ...r.allowedTaxonomy, [field]: next },
        };
      }),
    );
  };

  const toggleDeniedPath = (role: string, path: string) => {
    setRules((prev) =>
      prev.map((r) => {
        if (r.role !== role) return r;
        const current = r.deniedPaths ?? [];
        const next = current.includes(path)
          ? current.filter((p) => p !== path)
          : [...current, path];
        return { ...r, deniedPaths: next };
      }),
    );
  };

  const getEffectiveAccess = (role: string) => {
    const ov = roleOverrides[role] ?? "default";
    if (ov === "deny") return "denied";
    if (ov === "default")
      return defaultPolicy === "allow-all" ? "full-access" : "denied";
    return "custom";
  };

  return (
    <ConfigSection title="Access Control">
      {/* Default policy toggle */}
      <div className="flex items-center justify-between mb-3">
        <Type small muted>
          Default:
        </Type>
        <div className="inline-flex items-center gap-0.5 rounded-md border border-border p-0.5">
          <button
            onClick={() => setDefaultPolicy("allow-all")}
            className={cn(
              "px-2 py-0.5 text-[10px] font-medium rounded transition-colors",
              defaultPolicy === "allow-all"
                ? "bg-emerald-500/15 text-emerald-600"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            Allow all
          </button>
          <button
            onClick={() => setDefaultPolicy("deny-all")}
            className={cn(
              "px-2 py-0.5 text-[10px] font-medium rounded transition-colors",
              defaultPolicy === "deny-all"
                ? "bg-destructive/15 text-destructive"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            Deny all
          </button>
        </div>
      </div>

      {/* Per-role rows */}
      {MOCK_ALL_ROLES.map((role) => {
        const ov = roleOverrides[role] ?? "default";
        const effective = getEffectiveAccess(role);
        const isExpanded = expandedRole === role && ov === "custom";
        const rule = rules.find((r) => r.role === role);

        return (
          <div key={role} className="mb-1.5 last:mb-0">
            <div className="flex items-center justify-between py-1.5 px-2 rounded-md hover:bg-muted/30 transition-colors">
              <div className="flex items-center gap-2">
                <div
                  className={cn(
                    "h-1.5 w-1.5 rounded-full shrink-0",
                    effective === "full-access" && "bg-emerald-500",
                    effective === "custom" && "bg-amber-500",
                    effective === "denied" && "bg-muted-foreground/30",
                  )}
                />
                <Type small className="font-medium text-[11px]">
                  {role}
                </Type>
                <Badge
                  variant="outline"
                  className={cn(
                    "text-[9px] py-0",
                    effective === "full-access" &&
                      "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
                    effective === "custom" &&
                      "border-amber-500/50 text-amber-600 bg-amber-500/10 border-dashed",
                    effective === "denied" &&
                      "border-muted-foreground/30 text-muted-foreground bg-muted/30",
                  )}
                >
                  {effective === "full-access" && "Full access"}
                  {effective === "custom" && "Custom"}
                  {effective === "denied" && "Denied"}
                </Badge>
              </div>
              <div className="flex items-center gap-1">
                {ov === "custom" && (
                  <button
                    onClick={() => setExpandedRole(isExpanded ? null : role)}
                    className="px-1.5 py-0.5 rounded text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
                  >
                    {isExpanded ? "Collapse" : "Edit"}
                  </button>
                )}
                <button
                  onClick={() => cycleOverride(role)}
                  className={cn(
                    "px-2 py-0.5 rounded-md border text-[10px] font-medium transition-colors",
                    ov === "default" &&
                      "border-border text-muted-foreground hover:border-foreground/30",
                    ov === "custom" &&
                      "border-amber-500/50 text-amber-600 bg-amber-500/10",
                    ov === "deny" &&
                      "border-destructive/50 text-destructive bg-destructive/10",
                  )}
                >
                  {ov === "default"
                    ? "Default"
                    : ov === "custom"
                      ? "Custom"
                      : "Deny"}
                </button>
              </div>
            </div>

            {/* Expanded custom config */}
            {isExpanded && rule && (
              <div className="ml-5 mt-1 mb-2 pl-3 border-l-2 border-border space-y-2">
                {/* Allowed taxonomy */}
                {taxonomyFields.map((field) => {
                  const allValues = getTaxonomyValues(field);
                  const selected = rule.allowedTaxonomy?.[field] ?? [];
                  return (
                    <div key={field}>
                      <Type
                        small
                        muted
                        className="text-[10px] font-medium mb-1 block"
                      >
                        {field}
                      </Type>
                      <div className="flex flex-wrap gap-1">
                        {allValues.map((value) => {
                          const isActive = selected.includes(value);
                          return (
                            <button
                              key={value}
                              onClick={() =>
                                toggleTaxonomyValue(role, field, value)
                              }
                              className={cn(
                                "px-1.5 py-0.5 rounded-md text-[10px] border transition-colors",
                                isActive
                                  ? "border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
                                  : "border-border text-muted-foreground hover:text-foreground hover:border-foreground/30",
                              )}
                            >
                              {value}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  );
                })}

                {/* Denied paths */}
                <div>
                  <Type
                    small
                    muted
                    className="text-[10px] font-medium mb-1 block"
                  >
                    Denied paths
                  </Type>
                  <div className="flex flex-wrap gap-1">
                    {ALL_FOLDER_PATHS.map((path) => {
                      const isDenied = (rule.deniedPaths ?? []).includes(path);
                      return (
                        <button
                          key={path}
                          onClick={() => toggleDeniedPath(role, path)}
                          className={cn(
                            "px-1.5 py-0.5 rounded-md text-[10px] border transition-colors font-mono",
                            isDenied
                              ? "border-destructive/50 text-destructive bg-destructive/10"
                              : "border-border text-muted-foreground hover:text-foreground hover:border-foreground/30",
                          )}
                        >
                          {path}
                        </button>
                      );
                    })}
                  </div>
                </div>
              </div>
            )}

            {/* Collapsed summary for custom roles */}
            {!isExpanded && ov === "custom" && rule && (
              <div className="ml-5 pl-3 mt-0.5 flex flex-wrap gap-1">
                {rule.allowedTaxonomy &&
                  Object.entries(rule.allowedTaxonomy).flatMap(
                    ([field, values]) =>
                      values.map((value) => (
                        <Badge
                          key={`${field}-${value}`}
                          variant="secondary"
                          className="text-[9px] py-0 border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
                        >
                          {field}: {value}
                        </Badge>
                      )),
                  )}
                {rule.deniedPaths?.map((p) => (
                  <Badge
                    key={p}
                    variant="secondary"
                    className="text-[9px] py-0 border-destructive/50 text-destructive bg-destructive/10 font-mono"
                  >
                    ✕ {p}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </ConfigSection>
  );
}

function ConfigSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <Type small muted className="font-medium mb-1.5 block">
        {title}
      </Type>
      {children}
    </div>
  );
}

function ConfigField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <span className="text-muted-foreground">{label}:</span>{" "}
      <span className="font-medium text-foreground">{value}</span>
    </div>
  );
}

function FolderDetail({
  folder,
  path,
  config,
}: {
  folder: ContextFolder;
  path: string[];
  config: DocsMcpConfig | null;
}) {
  const counts = countItems(folder);
  const drafts = countDrafts(folder);
  const localConfigFile = findFile(folder, ".docs-mcp.json");

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <Icon name="folder" className="h-4 w-4 text-muted-foreground" />
          <Type variant="subheading">
            {path.length === 0 ? "docs" : path[path.length - 1]}
          </Type>
          {drafts > 0 && (
            <Badge
              variant="outline"
              className="border-amber-500/50 text-amber-600 bg-amber-500/10"
            >
              {drafts} draft{drafts !== 1 && "s"}
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
          <span>
            {counts.folders} folder{counts.folders !== 1 && "s"}
          </span>
          <span>
            {counts.files} file{counts.files !== 1 && "s"}
          </span>
        </div>
      </div>

      {config && (
        <div className="p-4 border-b border-border">
          <Type small muted className="font-medium mb-2 block">
            Effective Configuration
            {localConfigFile && (
              <Badge variant="outline" className="ml-2">
                local
              </Badge>
            )}
          </Type>
          <div className="space-y-2">
            {config.strategy && (
              <div className="text-xs">
                <span className="text-muted-foreground">Chunking:</span>{" "}
                <span className="font-medium text-foreground">
                  {config.strategy.chunk_by}
                </span>
                {config.strategy.max_chunk_size && (
                  <span className="text-muted-foreground">
                    {" "}
                    (max {config.strategy.max_chunk_size.toLocaleString()})
                  </span>
                )}
              </div>
            )}
            {config.metadata && Object.keys(config.metadata).length > 0 && (
              <div className="flex flex-wrap gap-1">
                {Object.entries(config.metadata).map(([key, value]) => (
                  <Badge key={key} variant="secondary">
                    {key}: {value}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      <div className="px-4 py-3 flex gap-2">
        <Button size="sm" variant="outline" className="flex-1">
          <Icon name="plus" className="h-3.5 w-3.5 mr-1.5" />
          New File
        </Button>
        <Button size="sm" variant="outline" className="flex-1">
          <Icon name="folder-plus" className="h-3.5 w-3.5 mr-1.5" />
          New Folder
        </Button>
      </div>
    </div>
  );
}

// ── Icon helpers ───────────────────────────────────────────────────────────

function getFileIcon(file: ContextFile): string {
  switch (file.kind) {
    case "skill":
      return "sparkles";
    case "mcp-docs-config":
      return "settings";
    case "markdown":
      return "file-text";
  }
}
