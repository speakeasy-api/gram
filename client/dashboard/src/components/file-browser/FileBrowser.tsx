/**
 * Reusable file browser component — tree on the left, detail on the right.
 * Used by the Context page and the Skill detail "Files" view.
 */

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Icon, ResizablePanel } from "@speakeasy-api/moonshine";
import {
  BotIcon,
  GitCommitHorizontalIcon,
  MoveRightIcon,
  UserIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import {
  type ContextFile,
  type ContextFolder,
  type ContextNode,
  type DiffLine,
  type DocFeedback,
  type DocsMcpConfig,
  type FileVersion,
  computeLineDiff,
  countDrafts,
  countItems,
  findFile,
  formatDate,
  formatFileSize,
  getEffectiveConfig,
  resolvePath,
} from "@/pages/context/mock-data";

// ── Public API ─────────────────────────────────────────────────────────────

export type FileBrowserProps = {
  root: ContextFolder;
  /** Optional label shown above the tree. Default: "Files". */
  label?: string;
  /** Hide the "Add" button in the tree header. */
  hideAddButton?: boolean;
  /** Compact mode — smaller default panel sizes. */
  compact?: boolean;
};

const RESIZABLE_PANEL_CLASS =
  "[&>[role='separator']]:w-px [&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:border-0 [&>[role='separator']]:hover:bg-primary [&>[role='separator']]:relative [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:cursor-col-resize";

export function FileBrowser({
  root,
  label = "Files",
  hideAddButton,
  compact,
}: FileBrowserProps) {
  const [selectedFile, setSelectedFile] = useState<ContextFile | null>(null);
  const [selectedPath, setSelectedPath] = useState<string[]>([]);

  const effectiveConfig = getEffectiveConfig(root, selectedPath);
  const selectedFolder = resolvePath(root, selectedPath);

  const handleFileSelect = (file: ContextFile, path: string[]) => {
    setSelectedFile(file);
    setSelectedPath(path);
  };

  const handleFolderSelect = (path: string[]) => {
    setSelectedFile(null);
    setSelectedPath(path);
  };

  return (
    <ResizablePanel
      direction="horizontal"
      className={cn("h-full", RESIZABLE_PANEL_CLASS)}
    >
      <ResizablePanel.Pane
        minSize={12}
        defaultSize={compact ? 25 : 18}
        maxSize={35}
      >
        <div className="h-full flex flex-col overflow-hidden">
          <div className="flex items-center justify-between px-3 py-2 border-b border-border">
            <Type
              small
              muted
              className="font-medium uppercase tracking-wider text-xs"
            >
              {label}
            </Type>
            {!hideAddButton && (
              <button
                className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
                title="Add content"
              >
                <Icon name="plus" className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          <div className="flex-1 overflow-y-auto py-1">
            <TreeNode
              node={root}
              depth={0}
              selectedFile={selectedFile}
              onFileSelect={handleFileSelect}
              onFolderSelect={handleFolderSelect}
              parentPath={[]}
            />
          </div>
        </div>
      </ResizablePanel.Pane>

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
  );
}

// ── Tree ────────────────────────────────────────────────────────────────────

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
        <Icon
          name={getFileIcon(node) as any}
          className="h-3.5 w-3.5 shrink-0"
        />
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
        className="flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm text-muted-foreground hover:text-foreground hover:bg-muted/50"
        style={{ paddingLeft: depth * 12 + 8 }}
      >
        <Icon
          name={expanded ? "chevron-down" : "chevron-right"}
          className="h-3 w-3 shrink-0"
        />
        <Icon name="folder" className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-medium">
          {depth === 0 ? node.name : node.name}
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

// ── File Detail ────────────────────────────────────────────────────────────

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
            name={getFileIcon(file) as any}
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
        file.kind === "markdown" &&
        displayContent && (
          <div className="p-4">
            <pre className="text-xs font-mono whitespace-pre-wrap text-foreground bg-muted/30 rounded-md p-3 overflow-auto max-h-[500px]">
              {displayContent}
            </pre>
          </div>
        )}

      {viewMode !== "diff" &&
        viewMode !== "history" &&
        file.kind === "skill" &&
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

// ── Version History ────────────────────────────────────────────────────────

function VersionHistory({ file }: { file: ContextFile }) {
  const [selectedVersion, setSelectedVersion] = useState<FileVersion | null>(
    null,
  );
  const [versionViewMode, setVersionViewMode] = useState<"content" | "diff">(
    "content",
  );

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
                    <span className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground bg-muted/50 rounded px-1 py-0">
                      <BotIcon className="h-2.5 w-2.5" />
                      {v.agent}
                    </span>
                  )}
                </div>
                <span className="text-foreground truncate block mt-0.5">
                  {v.message}
                </span>
                <div className="flex items-center gap-2 mt-1 text-muted-foreground">
                  <span className="inline-flex items-center gap-0.5">
                    <UserIcon className="h-2.5 w-2.5" />
                    {v.author}
                  </span>
                  {v.committer && v.committer !== v.author && (
                    <span className="inline-flex items-center gap-0.5">
                      <GitCommitHorizontalIcon className="h-2.5 w-2.5" />
                      {v.committer}
                    </span>
                  )}
                  <span>&middot;</span>
                  <span>{formatDate(v.updatedAt)}</span>
                  <span>&middot;</span>
                  <span>{formatFileSize(v.size)}</span>
                </div>
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

      {selectedVersion && selectedVersion.content && (
        <div className="border-t border-border">
          <div className="flex items-center gap-1 px-4 py-2 border-b border-border bg-muted/20">
            <span className="text-xs font-medium text-muted-foreground mr-2">
              v{selectedVersion.version}
            </span>
            <LayerToggle
              active={versionViewMode === "content"}
              onClick={() => setVersionViewMode("content")}
            >
              Content
            </LayerToggle>
            {previousVersion?.content && (
              <LayerToggle
                active={versionViewMode === "diff"}
                onClick={() => setVersionViewMode("diff")}
              >
                Diff from v{previousVersion.version}
              </LayerToggle>
            )}
          </div>
          <div className="p-4">
            {versionViewMode === "diff" && previousVersion?.content ? (
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

// ── Folder Detail ──────────────────────────────────────────────────────────

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

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <Icon name="folder" className="h-4 w-4 text-muted-foreground" />
          <Type variant="subheading">
            {path.length === 0 ? folder.name : path[path.length - 1]}
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

      {config && config.strategy && (
        <div className="p-4 border-b border-border">
          <Type small muted className="font-medium mb-2 block">
            Effective Configuration
          </Type>
          <div className="text-xs">
            <span className="text-muted-foreground">Chunking:</span>{" "}
            <span className="font-medium text-foreground">
              {config.strategy.chunk_by}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Shared helpers ─────────────────────────────────────────────────────────

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

export function LayerToggle({
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

function DiffView({ oldText, newText }: { oldText: string; newText: string }) {
  const lines = useMemo(
    () => computeLineDiff(oldText, newText),
    [oldText, newText],
  );
  return (
    <div className="rounded-md border border-border overflow-auto max-h-[500px] text-xs font-mono">
      {lines.map((line, i) => {
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
          <div key={i} className={cn("flex", bgClass)}>
            <span
              className={cn(
                "w-5 shrink-0 text-right pr-1 select-none",
                line.type === "same" ? "text-muted-foreground/50" : textClass,
              )}
            >
              {prefix}
            </span>
            <span
              className={cn("flex-1 px-2 py-px whitespace-pre-wrap", textClass)}
            >
              {line.content || "\u00A0"}
            </span>
          </div>
        );
      })}
    </div>
  );
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
