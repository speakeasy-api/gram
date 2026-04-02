import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { AddRepoDialog } from "./AddRepoDialog";
import {
  type ContextFile,
  type ContextFolder,
  type ContextNode,
  type DiffLine,
  type DocsMcpConfig,
  collectDrafts,
  collectSkills,
  computeLineDiff,
  countDrafts,
  countItems,
  findFile,
  formatDate,
  formatFileSize,
  getEffectiveConfig,
  hasDraft,
  MOCK_CONTEXT_TREE,
  parseSkillFrontmatter,
  resolvePath,
} from "./mock-data";

export function ContextRoot() {
  return <Outlet />;
}

export default function ContextPage() {
  const totalDrafts = useMemo(() => countDrafts(MOCK_CONTEXT_TREE), []);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Context</Page.Section.Title>
          <Page.Section.Description>
            Manage documentation, skills, and MCP search configuration for your
            project.
          </Page.Section.Description>
          <Page.Section.Body>
            <Tabs defaultValue="content">
              <TabsList className="bg-transparent p-0 h-auto border-b border-border rounded-none w-full justify-start items-stretch">
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
                <PageTabsTrigger value="playground">Playground</PageTabsTrigger>
              </TabsList>
              <TabsContent value="content" className="pt-4">
                <ContentTab />
              </TabsContent>
              <TabsContent value="pending-changes" className="pt-4">
                <PendingChangesTab />
              </TabsContent>
              <TabsContent value="skills" className="pt-4">
                <SkillsTab />
              </TabsContent>
              <TabsContent value="playground" className="pt-4">
                <PlaygroundTab />
              </TabsContent>
            </Tabs>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

// ── Content Tab ────────────────────────────────────────────────────────────

function ContentTab() {
  const [path, setPath] = useState<string[]>([]);
  const [selectedFile, setSelectedFile] = useState<ContextFile | null>(null);
  const [addRepoOpen, setAddRepoOpen] = useState(false);

  const currentFolder = resolvePath(MOCK_CONTEXT_TREE, path);
  const effectiveConfig = getEffectiveConfig(MOCK_CONTEXT_TREE, path);

  if (!currentFolder) {
    setPath([]);
    return null;
  }

  const navigateToFolder = (name: string) => {
    setSelectedFile(null);
    setPath((prev) => [...prev, name]);
  };

  const navigateUp = () => {
    setSelectedFile(null);
    setPath((prev) => prev.slice(0, -1));
  };

  const navigateToBreadcrumb = (index: number) => {
    setSelectedFile(null);
    setPath((prev) => prev.slice(0, index));
  };

  const handleNodeClick = (node: ContextNode) => {
    if (node.type === "folder") {
      navigateToFolder(node.name);
    } else {
      setSelectedFile(node);
    }
  };

  const sorted = [...currentFolder.children].sort((a, b) => {
    if (a.type !== b.type) return a.type === "folder" ? -1 : 1;
    if (a.type === "file" && b.type === "file") {
      if (a.kind === "mcp-docs-config") return -1;
      if (b.kind === "mcp-docs-config") return 1;
    }
    return a.name.localeCompare(b.name);
  });

  return (
    <>
      <div className="flex gap-6">
        {/* Left panel: file browser */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between mb-3">
            <Breadcrumbs path={path} onNavigate={navigateToBreadcrumb} />
            <Button size="sm" onClick={() => setAddRepoOpen(true)}>
              <Icon name="plus" className="h-3.5 w-3.5 mr-1.5" />
              Add
            </Button>
          </div>
          <div className="rounded-lg border border-border bg-card overflow-hidden">
            {path.length > 0 && (
              <button
                onClick={navigateUp}
                className="flex items-center gap-2 w-full px-4 py-2.5 text-sm text-muted-foreground hover:bg-muted/50 transition-colors border-b border-border"
              >
                <Icon name="arrow-left" className="h-4 w-4" />
                ..
              </button>
            )}
            {sorted.map((node) => (
              <NodeRow
                key={node.name}
                node={node}
                isSelected={
                  selectedFile?.type === "file" &&
                  selectedFile.name === node.name
                }
                onClick={() => handleNodeClick(node)}
              />
            ))}
            {sorted.length === 0 && (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground">
                This folder is empty.
              </div>
            )}
          </div>
        </div>

        {/* Right panel: detail view */}
        <div className="w-[420px] shrink-0">
          {selectedFile ? (
            <FileDetail file={selectedFile} />
          ) : (
            <FolderDetail
              folder={currentFolder}
              path={path}
              config={effectiveConfig}
            />
          )}
        </div>
      </div>

      <AddRepoDialog
        open={addRepoOpen}
        onOpenChange={setAddRepoOpen}
        onComplete={() => {
          // Mock: in real implementation this would trigger a refetch
        }}
      />
    </>
  );
}

// ── Pending Changes Tab ───────────────────────────────────────────────────

function PendingChangesTab() {
  const drafts = useMemo(() => collectDrafts(MOCK_CONTEXT_TREE), []);
  const [expandedFile, setExpandedFile] = useState<string | null>(null);

  if (drafts.length === 0) {
    return (
      <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[300px]">
        <div className="text-center space-y-2">
          <Icon
            name="check-circle"
            className="h-10 w-10 text-muted-foreground/50 mx-auto"
          />
          <Type variant="subheading" className="text-muted-foreground">
            No pending changes
          </Type>
          <Type small muted>
            All content is up to date.
          </Type>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <Type small muted>
          {drafts.length} file{drafts.length !== 1 && "s"} with unpublished
          drafts
        </Type>
        <Button size="sm">Publish All</Button>
      </div>
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {drafts.map(({ file, path }) => {
          const fullPath = [...path, file.name].join("/");
          const isExpanded = expandedFile === fullPath;
          return (
            <div
              key={fullPath}
              className="border-b border-border last:border-b-0"
            >
              <button
                onClick={() => setExpandedFile(isExpanded ? null : fullPath)}
                className="flex items-center gap-3 w-full px-4 py-3 text-sm hover:bg-muted/50 transition-colors"
              >
                <Icon
                  name={isExpanded ? "chevron-down" : "chevron-right"}
                  className="h-3.5 w-3.5 text-muted-foreground"
                />
                <Icon
                  name={getFileIcon(file)}
                  className="h-4 w-4 text-muted-foreground"
                />
                <span className="flex-1 text-left font-medium truncate">
                  <span className="text-muted-foreground">
                    {path.length > 0 ? path.join("/") + "/" : ""}
                  </span>
                  {file.name}
                </span>
                <DraftBadge />
                <span className="text-xs text-muted-foreground">
                  {file.draft?.author} &middot;{" "}
                  {formatDate(file.draft!.updatedAt)}
                </span>
                <div className="flex gap-1">
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-6 px-2 text-xs"
                    onClick={(e) => e.stopPropagation()}
                  >
                    Publish
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 px-2 text-xs text-destructive"
                    onClick={(e) => e.stopPropagation()}
                  >
                    Discard
                  </Button>
                </div>
              </button>
              {isExpanded && file.content && file.draft?.content && (
                <div className="px-4 pb-3">
                  <DiffView
                    oldText={file.content}
                    newText={file.draft.content}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Skills Tab ────────────────────────────────────────────────────────────

function SkillsTab() {
  const skills = useMemo(() => collectSkills(MOCK_CONTEXT_TREE), []);

  if (skills.length === 0) {
    return (
      <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[300px]">
        <div className="text-center space-y-2">
          <Icon
            name="sparkles"
            className="h-10 w-10 text-muted-foreground/50 mx-auto"
          />
          <Type variant="subheading" className="text-muted-foreground">
            No skills defined
          </Type>
          <Type small muted>
            Add SKILL.md files to your documentation to define skills.
          </Type>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <Type small muted>
        {skills.length} skill{skills.length !== 1 && "s"} found across your
        documentation
      </Type>
      <div className="grid gap-4">
        {skills.map(({ file, path }) => (
          <SkillCard
            key={[...path, file.name].join("/")}
            file={file}
            path={path}
          />
        ))}
      </div>
    </div>
  );
}

function SkillCard({ file, path }: { file: ContextFile; path: string[] }) {
  const { meta, body } = useMemo(
    () => parseSkillFrontmatter(file.content ?? ""),
    [file.content],
  );

  const location = [...path, file.name].join("/");

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Icon name="sparkles" className="h-4 w-4 text-primary" />
            <Type variant="subheading">{meta.name ?? file.name}</Type>
          </div>
          {file.draft && <DraftBadge />}
        </div>
        {meta.description && (
          <Type small muted className="mt-1 block">
            {meta.description}
          </Type>
        )}
      </div>

      {Object.keys(meta).length > 0 && (
        <div className="px-4 py-2.5 border-b border-border flex flex-wrap gap-1.5">
          {Object.entries(meta).map(([key, value]) => (
            <Badge key={key} variant="secondary">
              {key}: {value}
            </Badge>
          ))}
        </div>
      )}

      <div className="p-4">
        <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 max-h-[200px] overflow-auto">
          {body}
        </pre>
      </div>

      <div className="px-4 py-2.5 border-t border-border flex items-center justify-between">
        <Type small muted className="font-mono text-xs">
          {location}
        </Type>
        <span className="text-xs text-muted-foreground">
          Updated {formatDate(file.updatedAt)}
        </span>
      </div>
    </div>
  );
}

// ── Playground Tab (placeholder) ──────────────────────────────────────────

function PlaygroundTab() {
  return (
    <div className="flex items-center justify-center rounded-lg border border-dashed border-border bg-card h-[400px]">
      <div className="text-center space-y-2">
        <Icon
          name="message-circle"
          className="h-10 w-10 text-muted-foreground/50 mx-auto"
        />
        <Type variant="subheading" className="text-muted-foreground">
          Context Playground
        </Type>
        <Type small muted className="max-w-sm">
          Test search queries against your documentation context. Ask questions
          and see which chunks are retrieved.
        </Type>
      </div>
    </div>
  );
}

// ── Sub-components ─────────────────────────────────────────────────────────

function Breadcrumbs({
  path,
  onNavigate,
}: {
  path: string[];
  onNavigate: (index: number) => void;
}) {
  return (
    <nav className="flex items-center gap-1 text-sm text-muted-foreground">
      <button
        onClick={() => onNavigate(0)}
        className="hover:text-foreground transition-colors font-medium"
      >
        docs
      </button>
      {path.map((segment, i) => (
        <span key={i} className="flex items-center gap-1">
          <span>/</span>
          <button
            onClick={() => onNavigate(i + 1)}
            className="hover:text-foreground transition-colors font-medium"
          >
            {segment}
          </button>
        </span>
      ))}
    </nav>
  );
}

function NodeRow({
  node,
  isSelected,
  onClick,
}: {
  node: ContextNode;
  isSelected: boolean;
  onClick: () => void;
}) {
  const iconName = getNodeIcon(node);
  const counts = node.type === "folder" ? countItems(node) : null;
  const nodeHasDraft = hasDraft(node);
  const draftCount = node.type === "folder" ? countDrafts(node) : 0;

  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-3 w-full px-4 py-2.5 text-sm transition-colors border-b border-border last:border-b-0 ${
        isSelected
          ? "bg-primary/5 text-foreground"
          : "hover:bg-muted/50 text-foreground"
      }`}
    >
      <Icon
        name={iconName}
        className="h-4 w-4 shrink-0 text-muted-foreground"
      />
      <span className="flex-1 text-left truncate font-medium">{node.name}</span>
      {node.type === "file" && <FileKindBadge kind={node.kind} />}
      {node.type === "file" && nodeHasDraft && <DraftBadge />}
      {node.type === "folder" && draftCount > 0 && (
        <Badge
          variant="outline"
          className="border-amber-500/50 text-amber-600 bg-amber-500/10"
        >
          {draftCount} draft{draftCount !== 1 && "s"}
        </Badge>
      )}
      {counts && (
        <span className="text-xs text-muted-foreground">
          {counts.folders + counts.files} items
        </span>
      )}
      <span className="text-xs text-muted-foreground shrink-0">
        {formatDate(node.updatedAt)}
      </span>
      {node.type === "folder" && (
        <Icon
          name="chevron-right"
          className="h-3.5 w-3.5 text-muted-foreground"
        />
      )}
    </button>
  );
}

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
        (file.kind === "markdown" || file.kind === "skill") &&
        displayContent && (
          <div className="p-4">
            <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 overflow-auto max-h-[500px]">
              {displayContent}
            </pre>
          </div>
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

function getNodeIcon(node: ContextNode): string {
  if (node.type === "folder") return "folder";
  return getFileIcon(node);
}

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
