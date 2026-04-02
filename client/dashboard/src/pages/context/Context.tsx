import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Dialog } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
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
  ChevronDownIcon,
  MessageSquareIcon,
  PlusIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { AddRepoDialog } from "./AddRepoDialog";
import {
  type Annotation,
  type CaptureSettings,
  type ContextFile,
  type ContextFolder,
  type ContextNode,
  type DiffLine,
  type DocFeedback,
  type DocsMcpConfig,
  type DraftDocument,
  type RegistrySkill,
  collectDrafts,
  collectSkills,
  computeLineDiff,
  countDrafts,
  countItems,
  findFile,
  formatDate,
  formatFileSize,
  formatRelativeTime,
  formatTime,
  getEffectiveConfig,
  MOCK_CAPTURE_SETTINGS,
  MOCK_CONTEXT_TREE,
  MOCK_DRAFT_DOCUMENTS,
  MOCK_REGISTRY_SKILLS,
  MOCK_SEARCH_LOGS,
  MOCK_SKILL_INVOCATIONS,
  parseSkillFrontmatter,
  resolvePath,
  sourceLabel,
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
                <PageTabsTrigger value="skills">Skills</PageTabsTrigger>
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
            value="skills"
            className="flex-1 min-h-0 p-8 overflow-y-auto"
          >
            <SkillsTab />
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
            <div className="flex items-center justify-between px-3 py-2 border-b border-border">
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
            <div className="flex-1 overflow-y-auto py-1">
              <TreeNode
                node={MOCK_CONTEXT_TREE}
                depth={0}
                selectedFile={selectedFile}
                onFileSelect={handleFileSelect}
                onFolderSelect={handleFolderSelect}
                parentPath={[]}
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

function TreeNode({
  node,
  depth,
  selectedFile,
  onFileSelect,
  onFolderSelect,
  parentPath,
}: {
  node: ContextNode;
  depth: number;
  selectedFile: ContextFile | null;
  onFileSelect: (file: ContextFile, path: string[]) => void;
  onFolderSelect: (path: string[]) => void;
  parentPath: string[];
}) {
  const [expanded, setExpanded] = useState(depth < 1);

  if (node.type === "file") {
    const isSelected = selectedFile?.name === node.name;
    return (
      <button
        onClick={() => onFileSelect(node, parentPath)}
        className={cn(
          "flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm",
          isSelected
            ? "bg-primary/10 text-foreground"
            : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
        )}
        style={{ paddingLeft: depth * 12 + 8 }}
      >
        <Icon name={getFileIcon(node)} className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{node.name}</span>
        {node.draft && (
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
          onFolderSelect(folderPath);
        }}
        className={cn(
          "flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm",
          "text-muted-foreground hover:text-foreground hover:bg-muted/50",
        )}
        style={{ paddingLeft: depth * 12 + 8 }}
      >
        <Icon
          name={expanded ? "chevron-down" : "chevron-right"}
          className="h-3 w-3 shrink-0"
        />
        <Icon name="folder" className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-medium">
          {depth === 0 ? "docs" : node.name}
        </span>
        {draftCount > 0 && (
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
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ── Draft Documents Tab (Reddit-style) ────────────────────────────────────

function PendingChangesTab() {
  const drafts = MOCK_DRAFT_DOCUMENTS;
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [sortBy, setSortBy] = useState<"hot" | "new" | "top">("hot");

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
      {/* Sort bar */}
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
      </div>

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

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
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
              <Badge
                variant="outline"
                className="border-emerald-500/50 text-emerald-600 bg-emerald-500/10 text-[10px] px-1.5 py-0"
              >
                New Doc
              </Badge>
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

          {/* Comments */}
          <div className="p-4 space-y-3">
            {draft.comments.map((comment) => (
              <DraftCommentItem key={comment.id} comment={comment} />
            ))}
            <div className="flex gap-2 pt-2">
              <div className="flex-1 rounded-md border border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                Add a comment...
              </div>
              <Button size="sm" variant="outline" disabled>
                Comment
              </Button>
            </div>
          </div>
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

// ── Skills Tab (Registry) ─────────────────────────────────────────────────

type SkillFilter =
  | "all"
  | "corpus"
  | "captured"
  | "uploaded"
  | "pending-review";

function SkillsTab() {
  const [filter, setFilter] = useState<SkillFilter>("all");
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [captureSettings, setCaptureSettings] = useState<CaptureSettings>(
    MOCK_CAPTURE_SETTINGS,
  );
  const [expandedSkill, setExpandedSkill] = useState<string | null>(null);

  const skills = MOCK_REGISTRY_SKILLS;

  const filtered = useMemo(() => {
    if (filter === "all") return skills;
    if (filter === "pending-review")
      return skills.filter((s) => s.status === "pending-review");
    return skills.filter((s) => s.source === filter);
  }, [skills, filter]);

  const activeSkills = useMemo(
    () => skills.filter((s) => s.status === "active"),
    [skills],
  );

  const pendingCount = useMemo(
    () => skills.filter((s) => s.status === "pending-review").length,
    [skills],
  );

  const filterCounts: Record<SkillFilter, number> = useMemo(
    () => ({
      all: skills.length,
      corpus: skills.filter((s) => s.source === "corpus").length,
      captured: skills.filter((s) => s.source === "captured").length,
      uploaded: skills.filter((s) => s.source === "uploaded").length,
      "pending-review": pendingCount,
    }),
    [skills, pendingCount],
  );

  const filters: { value: SkillFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "corpus", label: "Corpus" },
    { value: "captured", label: "Captured" },
    { value: "uploaded", label: "Uploaded" },
    { value: "pending-review", label: "Pending Review" },
  ];

  return (
    <div className="space-y-6">
      {/* RemoteSkill tool preview */}
      <RemoteSkillToolPreview activeSkills={activeSkills} />

      {/* Filter bar + upload */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 rounded-lg border border-border bg-card p-1">
          {filters.map(({ value, label }) => (
            <button
              key={value}
              onClick={() => setFilter(value)}
              className={cn(
                "px-3 py-1.5 text-xs font-medium rounded-md transition-colors flex items-center gap-1.5",
                filter === value
                  ? "bg-foreground text-background"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
              )}
            >
              {label}
              <span
                className={cn(
                  "inline-flex items-center justify-center h-4 min-w-4 px-1 rounded-full text-[10px] font-medium leading-none",
                  filter === value
                    ? "bg-background/20 text-background"
                    : "bg-muted text-muted-foreground",
                  value === "pending-review" &&
                    filterCounts[value] > 0 &&
                    filter !== value &&
                    "bg-amber-500/20 text-amber-600",
                )}
              >
                {filterCounts[value]}
              </span>
            </button>
          ))}
        </div>
        <Button size="sm" onClick={() => setUploadDialogOpen(true)}>
          <Icon name="upload" className="h-3.5 w-3.5 mr-1.5" />
          Upload Skill
        </Button>
      </div>

      {/* Skill cards */}
      {filtered.length === 0 ? (
        <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[200px]">
          <div className="text-center space-y-2">
            <Icon
              name="sparkles"
              className="h-10 w-10 text-muted-foreground/50 mx-auto"
            />
            <Type variant="subheading" className="text-muted-foreground">
              No skills match this filter
            </Type>
            <Type small muted>
              Try a different filter or upload a new skill.
            </Type>
          </div>
        </div>
      ) : (
        <div className="grid gap-4">
          {filtered.map((skill) => (
            <RegistrySkillCard
              key={skill.id}
              skill={skill}
              isExpanded={expandedSkill === skill.id}
              onToggleExpand={() =>
                setExpandedSkill(expandedSkill === skill.id ? null : skill.id)
              }
            />
          ))}
        </div>
      )}

      {/* Capture settings */}
      <CaptureSettingsSection
        settings={captureSettings}
        onChange={setCaptureSettings}
      />

      {/* Upload dialog */}
      <UploadSkillDialog
        open={uploadDialogOpen}
        onOpenChange={setUploadDialogOpen}
      />
    </div>
  );
}

// ── RemoteSkill Tool Preview ──────────────────────────────────────────────

function RemoteSkillToolPreview({
  activeSkills,
}: {
  activeSkills: RegistrySkill[];
}) {
  const [expanded, setExpanded] = useState(false);

  const schemaExample = useMemo(
    () =>
      JSON.stringify(
        {
          name: "RemoteSkill",
          description:
            "Retrieve a skill from the Gram skills registry by ID. Returns the skill body as context for the agent.",
          inputSchema: {
            type: "object",
            properties: {
              skillID: {
                type: "string",
                enum: activeSkills.map((s) => s.id),
                description: "The ID of the skill to retrieve.",
              },
            },
            required: ["skillID"],
          },
        },
        null,
        2,
      ),
    [activeSkills],
  );

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-3 w-full px-4 py-3 text-sm hover:bg-muted/50 transition-colors"
      >
        <Icon
          name={expanded ? "chevron-down" : "chevron-right"}
          className="h-3.5 w-3.5 text-muted-foreground"
        />
        <Icon name="settings" className="h-4 w-4 text-primary" />
        <Type variant="subheading" className="flex-1 text-left">
          RemoteSkill Tool
        </Type>
        <Badge variant="default">
          {activeSkills.length} active skill
          {activeSkills.length !== 1 && "s"}
        </Badge>
      </button>

      {expanded && (
        <div className="border-t border-border">
          <div className="px-4 py-3 border-b border-border">
            <Type small muted className="block mb-2">
              This is how agents see the skills registry as a tool. Active
              skills appear as enum values in the skillID parameter.
            </Type>
            <div className="space-y-1.5">
              {activeSkills.map((skill) => (
                <div key={skill.id} className="flex items-center gap-2 text-xs">
                  <code className="font-mono text-foreground bg-muted/50 px-1.5 py-0.5 rounded">
                    {skill.id}
                  </code>
                  <span className="text-muted-foreground">
                    {skill.description}
                  </span>
                </div>
              ))}
            </div>
          </div>
          <div className="p-4">
            <Type small muted className="font-medium mb-2 block">
              JSON Schema
            </Type>
            <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 max-h-[300px] overflow-auto">
              {schemaExample}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Registry Skill Card ───────────────────────────────────────────────────

function RegistrySkillCard({
  skill,
  isExpanded,
  onToggleExpand,
}: {
  skill: RegistrySkill;
  isExpanded: boolean;
  onToggleExpand: () => void;
}) {
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      {/* Header */}
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Icon name="sparkles" className="h-4 w-4 text-primary" />
            <Type variant="subheading">{skill.name}</Type>
            <SkillStatusBadge status={skill.status} />
            <SkillSourceBadge source={skill.source} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">
              {skill.invocations.toLocaleString()} invocations
            </span>
            {skill.status === "pending-review" && (
              <Button size="sm" className="h-7 text-xs">
                Promote
              </Button>
            )}
            {skill.status === "active" ? (
              <Button size="sm" variant="outline" className="h-7 text-xs">
                Disable
              </Button>
            ) : skill.status === "disabled" ? (
              <Button size="sm" variant="outline" className="h-7 text-xs">
                Enable
              </Button>
            ) : null}
            <Button size="sm" variant="ghost" className="h-7 text-xs">
              <Icon name="download" className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <Type small muted className="mt-1 block">
          {skill.description}
        </Type>
        {skill.source === "captured" && skill.capturedFrom && (
          <Type small muted className="mt-1 block text-xs">
            Captured from{" "}
            <span className="font-medium text-foreground">
              {skill.capturedFrom.agentName}
            </span>{" "}
            in session{" "}
            <code className="font-mono bg-muted/50 px-1 py-0.5 rounded text-foreground">
              {skill.capturedFrom.sessionId}
            </code>
          </Type>
        )}
      </div>

      {/* Frontmatter badges */}
      {Object.keys(skill.frontmatter).length > 0 && (
        <div className="px-4 py-2.5 border-b border-border flex flex-wrap gap-1.5">
          {Object.entries(skill.frontmatter).map(([key, value]) => (
            <Badge key={key} variant="secondary">
              {key}: {value}
            </Badge>
          ))}
        </div>
      )}

      {/* Expandable body with SkillPreview */}
      <button
        onClick={onToggleExpand}
        className="flex items-center gap-2 w-full px-4 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/30 transition-colors"
      >
        <Icon
          name={isExpanded ? "chevron-down" : "chevron-right"}
          className="h-3 w-3"
        />
        {isExpanded ? "Hide" : "Show"} skill body
      </button>
      {isExpanded && (
        <div className="px-4 pb-4">
          <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 max-h-[200px] overflow-auto">
            {skill.body}
          </pre>
        </div>
      )}

      {/* Footer */}
      <div className="px-4 py-2.5 border-t border-border flex items-center justify-between">
        <div className="flex items-center gap-3">
          {skill.path && (
            <Type small muted className="font-mono text-xs">
              {skill.path}
            </Type>
          )}
          <span className="text-xs text-muted-foreground">
            by {skill.author}
          </span>
        </div>
        <span className="text-xs text-muted-foreground">
          Updated {formatDate(skill.updatedAt)}
        </span>
      </div>
    </div>
  );
}

function SkillStatusBadge({ status }: { status: RegistrySkill["status"] }) {
  switch (status) {
    case "active":
      return (
        <Badge
          variant="outline"
          className="border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
        >
          Active
        </Badge>
      );
    case "pending-review":
      return (
        <Badge
          variant="outline"
          className="border-amber-500/50 text-amber-600 bg-amber-500/10"
        >
          Pending Review
        </Badge>
      );
    case "disabled":
      return (
        <Badge
          variant="outline"
          className="border-muted-foreground/50 text-muted-foreground bg-muted/30"
        >
          Disabled
        </Badge>
      );
  }
}

function SkillSourceBadge({ source }: { source: RegistrySkill["source"] }) {
  switch (source) {
    case "corpus":
      return <Badge variant="secondary">Corpus</Badge>;
    case "captured":
      return <Badge variant="secondary">Captured</Badge>;
    case "uploaded":
      return <Badge variant="secondary">Uploaded</Badge>;
  }
}

// ── Capture Settings ──────────────────────────────────────────────────────

function CaptureSettingsSection({
  settings,
  onChange,
}: {
  settings: CaptureSettings;
  onChange: (settings: CaptureSettings) => void;
}) {
  const toggle = (key: keyof CaptureSettings) => {
    onChange({ ...settings, [key]: !settings[key] });
  };

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <Type variant="subheading">Capture Settings</Type>
        <Type small muted className="mt-1 block">
          Configure how skills are automatically captured from agent sessions.
        </Type>
      </div>
      <div className="divide-y divide-border">
        <CaptureToggle
          label="Enable skill capture"
          description="Automatically extract skills from agent conversations"
          checked={settings.enabled}
          onChange={() => toggle("enabled")}
        />
        <CaptureToggle
          label="Capture project-level skills"
          description="Capture skills scoped to this project"
          checked={settings.captureProjectSkills}
          onChange={() => toggle("captureProjectSkills")}
          disabled={!settings.enabled}
        />
        <CaptureToggle
          label="Capture user-level skills"
          description="Capture skills scoped to individual users"
          checked={settings.captureUserSkills}
          onChange={() => toggle("captureUserSkills")}
          disabled={!settings.enabled}
        />
        <CaptureToggle
          label="Honor x-gram-ignore frontmatter"
          description="Skip files with x-gram-ignore: true in their frontmatter"
          checked={settings.ignoreWithFrontmatter}
          onChange={() => toggle("ignoreWithFrontmatter")}
          disabled={!settings.enabled}
        />
      </div>
    </div>
  );
}

function CaptureToggle({
  label,
  description,
  checked,
  onChange,
  disabled,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex items-center justify-between px-4 py-3",
        disabled && "opacity-50",
      )}
    >
      <div>
        <Type small className="font-medium block">
          {label}
        </Type>
        <Type small muted className="block">
          {description}
        </Type>
      </div>
      <Switch
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
      />
    </div>
  );
}

// ── Upload Skill Dialog ───────────────────────────────────────────────────

function UploadSkillDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [content, setContent] = useState("");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-lg">
        <Dialog.Header>
          <Dialog.Title>Upload Skill</Dialog.Title>
          <Dialog.Description>
            Paste or write a SKILL.md file to add it to the registry. Include
            YAML frontmatter with name and description fields.
          </Dialog.Description>
        </Dialog.Header>
        <div className="py-4">
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder={`---\nname: my-skill\ndescription: Description of the skill\n---\n\nSkill instructions here...`}
            className="w-full h-48 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm font-mono resize-none focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
        <Dialog.Footer>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => {
              // Mock: would upload the skill
              onOpenChange(false);
              setContent("");
            }}
            disabled={!content.trim()}
          >
            Upload
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

// ── Observability Tab ─────────────────────────────────────────────────────

function ObservabilityTab() {
  const [subTab, setSubTab] = useState<"search-logs" | "skill-invocations">(
    "search-logs",
  );

  const searchStats = useMemo(() => {
    const total = MOCK_SEARCH_LOGS.length;
    const avgLatency =
      total > 0
        ? Math.round(
            MOCK_SEARCH_LOGS.reduce((sum, l) => sum + l.latencyMs, 0) / total,
          )
        : 0;
    return { total, avgLatency };
  }, []);

  const skillStats = useMemo(() => {
    const total = MOCK_SKILL_INVOCATIONS.length;
    const successes = MOCK_SKILL_INVOCATIONS.filter((s) => s.success).length;
    const successRate = total > 0 ? Math.round((successes / total) * 100) : 0;
    return { total, successRate };
  }, []);

  return (
    <div className="space-y-4">
      {/* Summary stats */}
      <div className="grid grid-cols-4 gap-4">
        <StatCard label="Total Searches" value={String(searchStats.total)} />
        <StatCard
          label="Avg Search Latency"
          value={`${searchStats.avgLatency}ms`}
        />
        <StatCard label="Skill Invocations" value={String(skillStats.total)} />
        <StatCard
          label="Skill Success Rate"
          value={`${skillStats.successRate}%`}
        />
      </div>

      {/* Sub-tab toggle */}
      <div className="flex items-center gap-1">
        <LayerToggle
          active={subTab === "search-logs"}
          onClick={() => setSubTab("search-logs")}
        >
          Search Logs
        </LayerToggle>
        <LayerToggle
          active={subTab === "skill-invocations"}
          onClick={() => setSubTab("skill-invocations")}
        >
          Skill Invocations
        </LayerToggle>
      </div>

      {subTab === "search-logs" ? (
        <SearchLogsTable />
      ) : (
        <SkillInvocationsTable />
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <Type small muted className="block">
        {label}
      </Type>
      <Type variant="subheading" className="mt-1 block">
        {value}
      </Type>
    </div>
  );
}

function LatencyBadge({ ms }: { ms: number }) {
  const variant = ms < 40 ? "default" : ms < 60 ? "secondary" : "destructive";
  return <Badge variant={variant}>{ms}ms</Badge>;
}

function SearchLogsTable() {
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Time
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Query
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Filters
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Results
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Top Chunk
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Latency
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Agent
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Session
              </th>
            </tr>
          </thead>
          <tbody>
            {MOCK_SEARCH_LOGS.map((log) => (
              <tr
                key={log.id}
                className="border-b border-border last:border-b-0 hover:bg-muted/50 transition-colors"
              >
                <td className="px-4 py-2.5 text-muted-foreground whitespace-nowrap">
                  {formatTime(log.timestamp)}
                </td>
                <td className="px-4 py-2.5 font-medium max-w-[200px] truncate">
                  {log.query}
                </td>
                <td className="px-4 py-2.5">
                  {log.filters ? (
                    <div className="flex flex-wrap gap-1">
                      {Object.entries(log.filters).map(([k, v]) => (
                        <Badge key={k} variant="secondary">
                          {k}: {v}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <span className="text-muted-foreground">&mdash;</span>
                  )}
                </td>
                <td className="px-4 py-2.5">{log.resultsCount}</td>
                <td className="px-4 py-2.5">
                  <code className="font-mono text-xs text-muted-foreground">
                    {log.topChunkPath}
                  </code>
                </td>
                <td className="px-4 py-2.5">
                  <LatencyBadge ms={log.latencyMs} />
                </td>
                <td className="px-4 py-2.5">{log.agentName}</td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                  {log.sessionId.slice(0, 8)}...
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SkillInvocationsTable() {
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Time
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Skill
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Agent
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Session
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Latency
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Status
              </th>
            </tr>
          </thead>
          <tbody>
            {MOCK_SKILL_INVOCATIONS.map((inv) => (
              <tr
                key={inv.id}
                className="border-b border-border last:border-b-0 hover:bg-muted/50 transition-colors"
              >
                <td className="px-4 py-2.5 text-muted-foreground whitespace-nowrap">
                  {formatTime(inv.timestamp)}
                </td>
                <td className="px-4 py-2.5 font-medium">{inv.skillName}</td>
                <td className="px-4 py-2.5">{inv.agentName}</td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                  {inv.sessionId.slice(0, 8)}...
                </td>
                <td className="px-4 py-2.5">
                  <LatencyBadge ms={inv.latencyMs} />
                </td>
                <td className="px-4 py-2.5">
                  {inv.success ? (
                    <Icon
                      name="check-circle"
                      className="h-4 w-4 text-emerald-500"
                    />
                  ) : (
                    <Icon
                      name="x-circle"
                      className="h-4 w-4 text-destructive"
                    />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

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
  const [showHistory, setShowHistory] = useState(false);
  const [viewMode, setViewMode] = useState<"published" | "draft" | "diff">(
    file.draft ? "diff" : "published",
  );

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
          {file.source && <SourceBadge source={file.source} />}
          {file.draft && <DraftBadge />}
        </div>
        <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
          <span>{formatFileSize(file.size)}</span>
          <span>Updated {formatDate(file.updatedAt)}</span>
          <button
            onClick={() => setShowHistory((v) => !v)}
            className="flex items-center gap-1 hover:text-foreground transition-colors"
          >
            <Icon name="history" className="h-3 w-3" />
            {file.versions.length} version{file.versions.length !== 1 && "s"}
          </button>
        </div>
        {file.draft && (
          <div className="flex items-center gap-1 mt-2">
            <LayerToggle
              active={viewMode === "published"}
              onClick={() => setViewMode("published")}
            >
              Published
            </LayerToggle>
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
          </div>
        )}
      </div>

      {showHistory && <VersionHistory file={file} />}

      {!showHistory &&
        viewMode === "diff" &&
        file.draft?.content &&
        file.content && (
          <div className="p-4">
            <DiffView oldText={file.content} newText={file.draft.content} />
          </div>
        )}

      {!showHistory &&
        viewMode !== "diff" &&
        file.kind === "mcp-docs-config" &&
        file.config && <ConfigDetail config={file.config} />}

      {!showHistory &&
        viewMode !== "diff" &&
        file.kind === "skill" &&
        displayContent && (
          <div className="p-4">
            <SkillPreview content={displayContent} compact />
          </div>
        )}

      {!showHistory &&
        viewMode !== "diff" &&
        file.kind === "markdown" &&
        displayContent && (
          <div className="p-4">
            <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 overflow-auto max-h-[500px]">
              {displayContent}
            </pre>
          </div>
        )}

      {file.feedback && <FeedbackSection feedback={file.feedback} />}

      {file.annotations && file.annotations.length > 0 && (
        <AnnotationsSection annotations={file.annotations} />
      )}

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

const SOURCE_BADGE_CLASSES: Record<string, string> = {
  github: "border-purple-500/50 text-purple-600 bg-purple-500/10",
  cli: "border-blue-500/50 text-blue-600 bg-blue-500/10",
  agent: "border-emerald-500/50 text-emerald-600 bg-emerald-500/10",
  manual: "border-muted-foreground/50 text-muted-foreground bg-muted/50",
};

function SourceBadge({
  source,
}: {
  source: NonNullable<ContextFile["source"]>;
}) {
  return (
    <Badge variant="outline" className={SOURCE_BADGE_CLASSES[source] ?? ""}>
      {sourceLabel(source)}
    </Badge>
  );
}

// ── Feedback Section ──────────────────────────────────────────────────────

function FeedbackSection({ feedback }: { feedback: DocFeedback }) {
  return (
    <div className="px-4 py-3 border-t border-border space-y-2">
      <Type small muted className="font-medium block">
        Feedback
      </Type>
      <div className="flex items-center gap-3">
        <Button
          size="sm"
          variant={feedback.userVote === "up" ? "default" : "outline"}
          className="h-7 px-2 gap-1.5"
        >
          <ThumbsUpIcon className="h-3.5 w-3.5" />
          <span className="text-xs">{feedback.upvotes}</span>
        </Button>
        <Button
          size="sm"
          variant={feedback.userVote === "down" ? "default" : "outline"}
          className="h-7 px-2 gap-1.5"
        >
          <ThumbsDownIcon className="h-3.5 w-3.5" />
          <span className="text-xs">{feedback.downvotes}</span>
        </Button>
      </div>
      {feedback.labels.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {feedback.labels.map((label) => (
            <Badge key={label} variant="secondary">
              {label}
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Annotations Section ───────────────────────────────────────────────────

function AnnotationsSection({ annotations }: { annotations: Annotation[] }) {
  return (
    <div className="border-t border-border">
      <Collapsible>
        <CollapsibleTrigger className="flex items-center justify-between w-full px-4 py-3 text-sm hover:bg-muted/50 transition-colors">
          <div className="flex items-center gap-2">
            <MessageSquareIcon className="h-4 w-4 text-muted-foreground" />
            <Type small muted className="font-medium">
              Annotations ({annotations.length})
            </Type>
          </div>
          <ChevronDownIcon className="h-4 w-4 text-muted-foreground transition-transform [[data-state=open]>&]:rotate-180" />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="px-4 pb-3 space-y-3">
            {annotations.map((annotation) => (
              <AnnotationItem key={annotation.id} annotation={annotation} />
            ))}
            <Button size="sm" variant="outline" className="w-full">
              <PlusIcon className="h-3.5 w-3.5 mr-1.5" />
              Add Annotation
            </Button>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}

function AnnotationItem({ annotation }: { annotation: Annotation }) {
  return (
    <div className="rounded-md border border-border bg-muted/30 p-3 space-y-1.5">
      <div className="flex items-center gap-2">
        <Type small className="font-medium">
          {annotation.author}
        </Type>
        <Badge
          variant={annotation.authorType === "agent" ? "default" : "secondary"}
        >
          {annotation.authorType === "agent" ? "Agent" : "Human"}
        </Badge>
        <span className="ml-auto text-xs text-muted-foreground">
          {formatDate(annotation.createdAt)}
        </span>
      </div>
      <Type small muted>
        {annotation.content}
      </Type>
    </div>
  );
}

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
  return (
    <div className="max-h-[300px] overflow-y-auto">
      {file.versions.map((v) => (
        <div
          key={v.version}
          className="flex items-center gap-3 px-4 py-2.5 text-xs border-b border-border last:border-b-0 hover:bg-muted/30 transition-colors"
        >
          <span className="font-mono text-muted-foreground w-6 shrink-0">
            v{v.version}
          </span>
          <div className="flex-1 min-w-0">
            <span className="text-foreground truncate block">{v.message}</span>
            <span className="text-muted-foreground">
              {v.author} &middot; {formatDate(v.updatedAt)} &middot;{" "}
              {formatFileSize(v.size)}
            </span>
          </div>
          <Button size="sm" variant="ghost" className="h-6 px-2 text-xs">
            Restore
          </Button>
        </div>
      ))}
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
        <ConfigSection title="Access Control">
          {config.accessControl.map((rule) => (
            <div
              key={rule.role}
              className="text-xs bg-muted/30 rounded-md p-2.5 mb-1.5 last:mb-0"
            >
              <Type small className="font-bold block mb-1.5">
                {rule.role}
              </Type>
              {rule.allowedTaxonomy &&
                Object.keys(rule.allowedTaxonomy).length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1.5">
                    {Object.entries(rule.allowedTaxonomy).flatMap(
                      ([field, values]) =>
                        values.map((value) => (
                          <Badge
                            key={`${field}-${value}`}
                            variant="secondary"
                            className="border-emerald-500/50 text-emerald-600 bg-emerald-500/10"
                          >
                            {field}: {value}
                          </Badge>
                        )),
                    )}
                  </div>
                )}
              {rule.deniedPaths && rule.deniedPaths.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {rule.deniedPaths.map((deniedPath) => (
                    <Badge
                      key={deniedPath}
                      variant="secondary"
                      className="border-destructive/50 text-destructive bg-destructive/10"
                    >
                      denied: {deniedPath}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          ))}
        </ConfigSection>
      )}
    </div>
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
