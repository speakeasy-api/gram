import { Page } from "@/components/page-layout";
import { CorpusDiffEditor, CorpusEditor } from "@/components/monaco-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Skeleton } from "@/components/ui/skeleton";
import { useProject, useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { Dialog } from "@/components/ui/dialog";
import { PageTabsTrigger, Tabs, TabsList } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import {
  cloneCorpus,
  type CorpusRepoRef,
  getCorpusRemoteURL,
  pushCorpusFile,
  readCommittedCorpusFile,
  readCorpusDirtyPaths,
  readCorpusFile,
  readCorpusFileVersions,
  readCorpusTree,
  writeCorpusFile,
} from "@/hooks/useCorpusFS";
import { fetchDrafts, publishDrafts, saveDraft } from "@/hooks/useDrafts";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
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
import {
  Link,
  Navigate,
  Outlet,
  useNavigate,
  useOutletContext,
  useParams,
} from "react-router";
import { AddRepoDialog } from "./AddRepoDialog";
import { AnnotationsPanel } from "./AnnotationsPanel";
import { FeedbackPanel } from "./FeedbackPanel";
import { ObservabilityTab } from "./ObservabilityTab";
import { buildCorpusQuickStart } from "./contextQuickStart";
import {
  type ContextFile,
  type ContextFolder,
  type ContextNode,
  type DocsMcpConfig,
  type DraftDocument,
  type FileVersion,
  collectDrafts,
  countDrafts,
  countItems,
  findFile,
  formatDate,
  formatFileSize,
  formatRelativeTime,
  getEffectiveConfig,
  MOCK_ALL_ROLES,
  parseSkillFrontmatter,
} from "./mock-data";

const RESIZABLE_PANEL_CLASS =
  "[&>[role='separator']]:w-px [&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:border-0 [&>[role='separator']]:hover:bg-primary [&>[role='separator']]:relative [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:cursor-col-resize";

type ContextRouteContext = {
  corpusTree: ContextFolder | null;
  corpusLoading: boolean;
  corpusError: string | null;
  dirtyPaths: Set<string>;
  corpusRemoteURL: string | null;
  projectSlug: string | null;
  corpusRef: CorpusRepoRef | null;
  sessionToken?: string;
  authorName: string;
  authorEmail: string;
  refreshDirtyPaths: () => Promise<void>;
  onPushed: () => Promise<void>;
  drafts: DraftDocument[];
  draftsLoading: boolean;
  ensureDraftsLoaded: () => Promise<DraftDocument[]>;
  refreshDrafts: () => Promise<DraftDocument[]>;
  upsertDraft: (draft: DraftDocument) => void;
  projectId: string;
  totalDrafts: number;
};

function useContextRouteContext() {
  return useOutletContext<ContextRouteContext>();
}

export function ContextIndexRedirect() {
  return <Navigate to="content" replace />;
}

export function ContextRoot() {
  const project = useProject();
  const { session, user } = useSession();
  const { orgSlug } = useSlugs();
  const routes = useRoutes();

  const corpusRef = useMemo(() => {
    if (!project.id || !project.slug || !orgSlug) {
      return null;
    }

    return {
      projectId: project.id,
      projectSlug: project.slug,
      orgSlug,
    };
  }, [orgSlug, project.id, project.slug]);

  // ── Corpus tree from git ────────────────────────────────────────────────
  const [corpusTree, setCorpusTree] = useState<ContextFolder | null>(null);
  const [corpusLoading, setCorpusLoading] = useState(true);
  const [corpusError, setCorpusError] = useState<string | null>(null);
  const [dirtyPaths, setDirtyPaths] = useState<Set<string>>(new Set());

  const loadCorpus = useCallback(async () => {
    if (!corpusRef) {
      setCorpusTree({
        type: "folder",
        name: "docs",
        children: [],
        updatedAt: new Date().toISOString(),
      });
      setCorpusLoading(false);
      setCorpusError(null);
      setDirtyPaths(new Set());
      return;
    }

    setCorpusLoading(true);
    setCorpusError(null);
    try {
      await cloneCorpus(corpusRef, session || undefined);
      const [children, nextDirtyPaths] = await Promise.all([
        readCorpusTree(corpusRef),
        readCorpusDirtyPaths(corpusRef),
      ]);
      setCorpusTree({
        type: "folder",
        name: "docs",
        children,
        updatedAt: new Date().toISOString(),
      });
      setDirtyPaths(new Set(nextDirtyPaths));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (
        msg.includes("Could not find") ||
        msg.includes("empty") ||
        msg.includes("404") ||
        msg.includes("HttpError")
      ) {
        setCorpusTree({
          type: "folder",
          name: "docs",
          children: [],
          updatedAt: new Date().toISOString(),
        });
        setDirtyPaths(new Set());
      } else {
        setCorpusError(msg);
      }
    } finally {
      setCorpusLoading(false);
    }
  }, [corpusRef, session]);

  useEffect(() => {
    void loadCorpus();
  }, [loadCorpus]);

  const handleRefreshDirtyPaths = useCallback(async () => {
    if (!corpusRef) {
      return;
    }

    const nextDirtyPaths = await readCorpusDirtyPaths(corpusRef);
    setDirtyPaths(new Set(nextDirtyPaths));
  }, [corpusRef]);

  // ── Drafts from API ────────────────────────────────────────────────────
  const [drafts, setDrafts] = useState<DraftDocument[]>([]);
  const [draftsLoading, setDraftsLoading] = useState(false);
  const [draftsLoaded, setDraftsLoaded] = useState(false);
  const draftsRequestRef = useRef<Promise<DraftDocument[]> | null>(null);

  const loadDrafts = useCallback(
    async (force = false) => {
      if (!project.id) {
        setDrafts([]);
        setDraftsLoaded(true);
        setDraftsLoading(false);
        return [];
      }

      if (!force) {
        if (draftsLoaded) {
          return drafts;
        }

        if (draftsRequestRef.current) {
          return draftsRequestRef.current;
        }
      }

      setDraftsLoading(true);
      const request = (async () => {
        try {
          const result = await fetchDrafts(project.id);
          setDrafts(result);
          setDraftsLoaded(true);
          return result;
        } catch {
          // Silently fall back to empty — API may not be deployed yet
          setDrafts([]);
          setDraftsLoaded(true);
          return [];
        } finally {
          setDraftsLoading(false);
          draftsRequestRef.current = null;
        }
      })();

      draftsRequestRef.current = request;
      return request;
    },
    [drafts, draftsLoaded, project.id],
  );

  const refreshDrafts = useCallback(() => {
    return loadDrafts(true);
  }, [loadDrafts]);

  const upsertDraft = useCallback((draft: DraftDocument) => {
    setDrafts((current) => {
      const existingIndex = current.findIndex((item) => item.id === draft.id);
      if (existingIndex === -1) {
        return [draft, ...current];
      }

      const next = [...current];
      next[existingIndex] = draft;
      return next;
    });
    setDraftsLoaded(true);
  }, []);

  const totalDrafts = drafts.filter((d) => d.status === "open").length;
  const activeTab = routes.context.pendingChanges.active
    ? "pending-changes"
    : routes.context.observability.active
      ? "observability"
      : "content";

  const outletContext = useMemo<ContextRouteContext>(
    () => ({
      corpusTree,
      corpusLoading,
      corpusError,
      dirtyPaths,
      corpusRemoteURL: project.id ? getCorpusRemoteURL(project.id) : null,
      projectSlug: project.slug || null,
      corpusRef,
      sessionToken: session || undefined,
      authorName: user.displayName || user.email || "Gram User",
      authorEmail: user.email || "corpus@getgram.ai",
      refreshDirtyPaths: handleRefreshDirtyPaths,
      onPushed: loadCorpus,
      drafts,
      draftsLoading,
      ensureDraftsLoaded: loadDrafts,
      refreshDrafts,
      upsertDraft,
      projectId: project.id,
      totalDrafts,
    }),
    [
      corpusError,
      corpusLoading,
      corpusRef,
      corpusTree,
      dirtyPaths,
      drafts,
      draftsLoading,
      loadDrafts,
      handleRefreshDirtyPaths,
      loadCorpus,
      project.id,
      project.slug,
      refreshDrafts,
      session,
      totalDrafts,
      upsertDraft,
      user.displayName,
      user.email,
    ],
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight noPadding overflowHidden>
        <Tabs value={activeTab} className="flex flex-col h-full">
          <div className="border-b">
            <div className="px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none items-stretch">
                <PageTabsTrigger value="content" asChild>
                  <Link to={routes.context.content.href()}>Content</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="pending-changes" asChild>
                  <Link to={routes.context.pendingChanges.href()}>
                    Pending Changes
                    {totalDrafts > 0 && (
                      <span className="ml-1.5 inline-flex items-center justify-center h-5 min-w-5 px-1 rounded-full bg-amber-500 text-white text-xs font-medium leading-none">
                        {totalDrafts}
                      </span>
                    )}
                  </Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="observability" asChild>
                  <Link to={routes.context.observability.href()}>
                    Observability
                  </Link>
                </PageTabsTrigger>
              </TabsList>
            </div>
          </div>
          <div className="flex-1 min-h-0">
            <Outlet context={outletContext} />
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

export function ContextContentPage() {
  const {
    authorEmail,
    authorName,
    corpusError,
    corpusLoading,
    corpusRef,
    corpusRemoteURL,
    corpusTree,
    dirtyPaths,
    drafts,
    ensureDraftsLoaded,
    refreshDirtyPaths,
    onPushed,
    projectSlug,
    sessionToken,
    upsertDraft,
  } = useContextRouteContext();

  return (
    <ContentTab
      tree={corpusTree}
      loading={corpusLoading}
      error={corpusError}
      dirtyPaths={dirtyPaths}
      corpusRemoteURL={corpusRemoteURL}
      projectSlug={projectSlug}
      corpusRef={corpusRef}
      sessionToken={sessionToken}
      authorName={authorName}
      authorEmail={authorEmail}
      drafts={drafts}
      ensureDraftsLoaded={ensureDraftsLoaded}
      onDraftSaved={upsertDraft}
      refreshDirtyPaths={refreshDirtyPaths}
      onPushed={onPushed}
    />
  );
}

export function ContextPendingChangesPage() {
  const {
    corpusRef,
    drafts,
    draftsLoading,
    ensureDraftsLoaded,
    projectId,
    refreshDrafts,
  } = useContextRouteContext();

  useEffect(() => {
    void ensureDraftsLoaded();
  }, [ensureDraftsLoaded]);

  return (
    <div className="h-full min-h-0 p-8 overflow-y-auto">
      <PendingChangesTab
        corpusRef={corpusRef}
        drafts={drafts}
        loading={draftsLoading}
        projectId={projectId}
        onPublished={() => {
          void refreshDrafts();
        }}
      />
    </div>
  );
}

export function ContextObservabilityPage() {
  return (
    <div className="h-full min-h-0 p-8 overflow-y-auto">
      <ObservabilityTab />
    </div>
  );
}

// ── Content Tab ────────────────────────────────────────────────────────────

const EMPTY_TREE: ContextFolder = {
  type: "folder",
  name: "docs",
  children: [],
  updatedAt: new Date().toISOString(),
};

function ContentTab({
  tree,
  loading,
  error,
  dirtyPaths,
  corpusRemoteURL,
  projectSlug,
  corpusRef,
  sessionToken,
  authorName,
  authorEmail,
  drafts,
  ensureDraftsLoaded,
  onDraftSaved,
  refreshDirtyPaths,
  onPushed,
}: {
  tree: ContextFolder | null;
  loading: boolean;
  error: string | null;
  dirtyPaths: Set<string>;
  corpusRemoteURL: string | null;
  projectSlug: string | null;
  corpusRef: CorpusRepoRef | null;
  sessionToken?: string;
  authorName: string;
  authorEmail: string;
  drafts: DraftDocument[];
  ensureDraftsLoaded: () => Promise<DraftDocument[]>;
  onDraftSaved: (draft: DraftDocument) => void;
  refreshDirtyPaths: () => Promise<void>;
  onPushed: () => Promise<void>;
}) {
  const [addRepoOpen, setAddRepoOpen] = useState(false);
  const [viewAsRole, setViewAsRole] = useState<string | null>(null);
  const routes = useRoutes();
  const navigate = useNavigate();
  const params = useParams();

  const contextTree = tree ?? EMPTY_TREE;
  const routePath = params["*"] ?? "";
  const requestedPath = useMemo(
    () => splitContextRoutePath(routePath),
    [routePath],
  );
  const selection = useMemo(
    () => resolveContextSelection(contextTree, requestedPath),
    [contextTree, requestedPath],
  );
  const selectedFile = selection.file;
  const selectedPath = selection.selectedPath;
  const selectedFolder = selection.folder;
  const effectiveConfig = getEffectiveConfig(contextTree, selectedPath);
  const selectedFilePath = useMemo(
    () => joinContextRoutePath(selection.canonicalSegments),
    [selection.canonicalSegments],
  );

  useEffect(() => {
    if (loading || error) {
      return;
    }

    const requested = joinContextRoutePath(requestedPath);
    const canonical = joinContextRoutePath(selection.canonicalSegments);
    if (requested === canonical) {
      return;
    }

    navigate(
      canonical
        ? routes.context.content.href(canonical)
        : routes.context.content.href(),
      { replace: true },
    );
  }, [
    error,
    loading,
    navigate,
    requestedPath,
    routes,
    selection.canonicalSegments,
  ]);

  const navigateToSelection = useCallback(
    (segments: string[]) => {
      const path = joinContextRoutePath(segments);
      navigate(
        path
          ? routes.context.content.href(path)
          : routes.context.content.href(),
      );
    },
    [navigate, routes],
  );

  const handleFileSelect = useCallback(
    (path: string[]) => {
      navigateToSelection(path);
    },
    [navigateToSelection],
  );

  const handleFolderSelect = useCallback(
    (path: string[]) => {
      navigateToSelection(path);
    },
    [navigateToSelection],
  );

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
              {loading ? (
                <ContextTreeSkeleton />
              ) : error ? (
                <div className="flex items-center justify-center py-8">
                  <Type small muted className="text-destructive">
                    {error}
                  </Type>
                </div>
              ) : (
                contextTree.children.map((child) => (
                  <TreeNode
                    key={child.name}
                    node={child}
                    depth={0}
                    selectedPath={selection.canonicalSegments}
                    selectedKind={selection.kind}
                    dirtyPaths={dirtyPaths}
                    onFileSelect={handleFileSelect}
                    onFolderSelect={handleFolderSelect}
                    parentPath={[]}
                    viewAsRole={viewAsRole}
                    configs={collectConfigs(contextTree)}
                  />
                ))
              )}
            </div>
          </div>
        </ResizablePanel.Pane>

        {/* Right: detail view */}
        <ResizablePanel.Pane minSize={40}>
          <div className="h-full min-h-0 p-6">
            {loading ? (
              <ContextDetailSkeleton />
            ) : selectedFile ? (
              <div className="h-full min-h-0">
                <FileDetail
                  file={selectedFile}
                  filePath={selectedFilePath}
                  corpusRef={corpusRef}
                  sessionToken={sessionToken}
                  authorName={authorName}
                  authorEmail={authorEmail}
                  drafts={drafts}
                  ensureDraftsLoaded={ensureDraftsLoaded}
                  onDraftSaved={onDraftSaved}
                  refreshDirtyPaths={refreshDirtyPaths}
                  onPushed={onPushed}
                />
              </div>
            ) : selectedFolder ? (
              <div className="h-full overflow-y-auto">
                <FolderDetail
                  folder={selectedFolder}
                  path={selectedPath}
                  config={effectiveConfig}
                  corpusRemoteURL={corpusRemoteURL}
                  projectSlug={projectSlug}
                />
              </div>
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

function ContextTreeSkeleton() {
  return (
    <div className="px-2 py-2 space-y-1.5">
      <Skeleton className="h-6 w-full rounded-sm" />
      <Skeleton className="h-6 w-[88%] rounded-sm" />
      <Skeleton className="h-6 w-[82%] rounded-sm" />
      <Skeleton className="h-6 w-[76%] rounded-sm ml-5" />
      <Skeleton className="h-6 w-[72%] rounded-sm ml-10" />
      <Skeleton className="h-6 w-[84%] rounded-sm ml-5" />
      <Skeleton className="h-6 w-[79%] rounded-sm" />
      <Skeleton className="h-6 w-[68%] rounded-sm ml-5" />
    </div>
  );
}

function ContextDetailSkeleton() {
  return (
    <div className="h-full min-h-0 rounded-lg border border-border bg-card overflow-hidden flex flex-col">
      <div className="px-4 py-3 border-b border-border space-y-3">
        <div className="flex items-center gap-2">
          <Skeleton className="h-4 w-4 rounded-sm" />
          <Skeleton className="h-5 w-56" />
        </div>
        <div className="flex items-center gap-3">
          <Skeleton className="h-3 w-16" />
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-3 w-40" />
        </div>
        <div className="flex items-center gap-2">
          <Skeleton className="h-6 w-20 rounded-md" />
          <Skeleton className="h-6 w-20 rounded-md" />
        </div>
      </div>
      <div className="flex-1 min-h-0 p-4">
        <div className="h-full rounded-md border border-border bg-muted/20 p-4">
          <div className="space-y-3">
            <Skeleton className="h-5 w-40" />
            {Array.from({ length: 14 }).map((_, index) => (
              <div key={index} className="flex items-center gap-4">
                <Skeleton className="h-4 w-6 shrink-0" />
                <Skeleton
                  className={cn(
                    "h-4",
                    index % 5 === 0
                      ? "w-1/3"
                      : index % 3 === 0
                        ? "w-2/3"
                        : "w-1/2",
                  )}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
      <div className="shrink-0 border-t border-border px-4 py-3 flex items-center justify-between gap-3">
        <Skeleton className="h-4 w-40" />
        <div className="flex items-center gap-2">
          <Skeleton className="h-8 w-20 rounded-md" />
          <Skeleton className="h-8 w-16 rounded-md" />
        </div>
      </div>
    </div>
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

type ContextSelection = {
  kind: "file" | "folder";
  canonicalSegments: string[];
  selectedPath: string[];
  folder: ContextFolder;
  file: ContextFile | null;
};

function splitContextRoutePath(path: string): string[] {
  return path.split("/").filter(Boolean);
}

function joinContextRoutePath(segments: string[]): string {
  return segments.join("/");
}

function pathsEqual(a: string[], b: string[]): boolean {
  return (
    a.length === b.length && a.every((segment, index) => segment === b[index])
  );
}

function resolveContextSelection(
  root: ContextFolder,
  segments: string[],
): ContextSelection {
  if (segments.length === 0) {
    return {
      kind: "folder",
      canonicalSegments: [],
      selectedPath: [],
      folder: root,
      file: null,
    };
  }

  let currentFolder = root;
  const folderPath: string[] = [];

  for (let index = 0; index < segments.length; index += 1) {
    const segment = segments[index];
    const child = currentFolder.children.find((node) => node.name === segment);

    if (!child) {
      return {
        kind: "folder",
        canonicalSegments: folderPath,
        selectedPath: folderPath,
        folder: currentFolder,
        file: null,
      };
    }

    if (child.type === "folder") {
      folderPath.push(segment);
      currentFolder = child;

      if (index === segments.length - 1) {
        return {
          kind: "folder",
          canonicalSegments: folderPath,
          selectedPath: folderPath,
          folder: currentFolder,
          file: null,
        };
      }

      continue;
    }

    return {
      kind: "file",
      canonicalSegments: [...folderPath, segment],
      selectedPath: folderPath,
      folder: currentFolder,
      file: child,
    };
  }

  return {
    kind: "folder",
    canonicalSegments: folderPath,
    selectedPath: folderPath,
    folder: currentFolder,
    file: null,
  };
}

/** Check if a path is denied for a role across all .docs-mcp.json configs. */
function isPathDeniedForRole(
  configs: DocsMcpConfig[],
  role: string,
  nodePath: string,
): boolean {
  return configs.some((config) => {
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
  selectedPath,
  selectedKind,
  dirtyPaths,
  onFileSelect,
  onFolderSelect,
  parentPath,
  viewAsRole,
  configs,
}: {
  node: ContextNode;
  depth: number;
  selectedPath: string[];
  selectedKind: "file" | "folder";
  dirtyPaths: Set<string>;
  onFileSelect: (path: string[]) => void;
  onFolderSelect: (path: string[]) => void;
  parentPath: string[];
  viewAsRole?: string | null;
  configs?: DocsMcpConfig[];
}) {
  const nodeSegments = [...parentPath, node.name];
  const selectedPrefix = selectedPath.slice(0, nodeSegments.length);
  const isOnSelectedPath = pathsEqual(nodeSegments, selectedPrefix);
  const [expanded, setExpanded] = useState(depth < 1 || isOnSelectedPath);

  useEffect(() => {
    if (isOnSelectedPath) {
      setExpanded(true);
    }
  }, [isOnSelectedPath]);

  // Build the path string for access control checks
  const nodePath = nodeSegments.join("/") + (node.type === "folder" ? "/" : "");
  const isDenied =
    viewAsRole != null &&
    nodePath &&
    isPathDeniedForRole(configs ?? [], viewAsRole, nodePath);

  // Files indent past the chevron column so they align with the folder name,
  // not the chevron. Folders: base + chevron(w-3) + gap(1.5) + folder-icon.
  // Files: base + spacer matching chevron+gap + file-icon.
  const INDENT_PX = 14;
  const CHEVRON_SPACER = 18; // 12px chevron + 6px gap

  if (node.type === "file") {
    const filePath = [...parentPath, node.name];
    const isSelected =
      selectedKind === "file" && pathsEqual(selectedPath, filePath);
    const isDirty = dirtyPaths.has(filePath.join("/"));
    return (
      <button
        onClick={() => !isDenied && onFileSelect(filePath)}
        className={cn(
          "flex items-center gap-1.5 w-full py-1 pr-2 text-xs transition-colors rounded-sm",
          isDenied && "opacity-30 cursor-not-allowed",
          !isDenied && isSelected && "bg-primary/10 text-foreground",
          !isDenied &&
            !isSelected &&
            "text-muted-foreground hover:text-foreground hover:bg-muted/50",
          !isDenied && isDirty && !isSelected && "bg-muted/35 text-foreground",
          !isDenied && isDirty && isSelected && "bg-primary/15",
        )}
        style={{ paddingLeft: depth * INDENT_PX + 8 + CHEVRON_SPACER }}
      >
        <Icon name={getFileIcon(node)} className="h-3.5 w-3.5 shrink-0" />
        <span className={cn("truncate", isDirty && "italic")}>{node.name}</span>
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
  const folderPath = [...parentPath, node.name];
  const isSelected =
    selectedKind === "folder" && pathsEqual(selectedPath, folderPath);
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
          !isDenied && isSelected
            ? "bg-primary/10 text-foreground"
            : !isDenied &&
                "text-muted-foreground hover:text-foreground hover:bg-muted/50",
        )}
        style={{ paddingLeft: depth * INDENT_PX + 8 }}
      >
        <Icon
          name={expanded ? "chevron-down" : "chevron-right"}
          className="h-3 w-3 shrink-0"
        />
        <Icon name="folder" className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate font-medium">{node.name}</span>
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
              selectedPath={selectedPath}
              selectedKind={selectedKind}
              dirtyPaths={dirtyPaths}
              onFileSelect={onFileSelect}
              onFolderSelect={onFolderSelect}
              parentPath={folderPath}
              viewAsRole={viewAsRole}
              configs={configs}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ── Draft Documents Tab (Reddit-style) ────────────────────────────────────

type YoloSchedule = "off" | "24h" | "weekly";

function PendingChangesTab({
  corpusRef,
  drafts,
  loading,
  projectId,
  onPublished,
}: {
  corpusRef: CorpusRepoRef | null;
  drafts: DraftDocument[];
  loading: boolean;
  projectId: string;
  onPublished: () => void;
}) {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [sortBy, setSortBy] = useState<"hot" | "new" | "top">("hot");
  const [yoloSchedule, setYoloSchedule] = useState<YoloSchedule>("off");
  const [publishing, setPublishing] = useState(false);

  const handlePublish = useCallback(
    async (draftId: string) => {
      if (!projectId || publishing) return;
      setPublishing(true);
      try {
        await publishDrafts(projectId, undefined, [draftId]);
        onPublished();
      } catch (err) {
        console.error("Failed to publish draft:", err);
      } finally {
        setPublishing(false);
      }
    },
    [projectId, publishing, onPublished],
  );

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

  if (loading) {
    return (
      <div className="max-w-4xl space-y-3">
        <div className="flex items-center gap-4">
          <Skeleton className="h-9 w-40" />
          <Skeleton className="h-4 w-24" />
          <Skeleton className="ml-auto h-8 w-28" />
        </div>
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, index) => (
            <div
              key={index}
              className="rounded-lg border border-border bg-card p-4 space-y-3"
            >
              <div className="flex items-center gap-3">
                <Skeleton className="h-5 w-20" />
                <Skeleton className="h-4 w-40" />
                <Skeleton className="ml-auto h-8 w-24" />
              </div>
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

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
            corpusRef={corpusRef}
            draft={draft}
            expanded={expandedId === draft.id}
            onToggle={() =>
              setExpandedId(expandedId === draft.id ? null : draft.id)
            }
            onPublish={handlePublish}
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
  corpusRef,
  draft,
  expanded,
  onToggle,
  onPublish,
}: {
  corpusRef: CorpusRepoRef | null;
  draft: DraftDocument;
  expanded: boolean;
  onToggle: () => void;
  onPublish?: (draftId: string) => void;
}) {
  const score = draft.upvotes - draft.downvotes;
  const isEdit = draft.filePath !== null;
  const [iterateState, setIterateState] = useState<IterateState>("idle");
  const [iteratePrompt, setIteratePrompt] = useState("");
  const [showPrompt, setShowPrompt] = useState(false);
  const [originalContent, setOriginalContent] = useState(
    draft.originalContent ?? "",
  );
  const [originalContentLoading, setOriginalContentLoading] = useState(false);
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

  useEffect(() => {
    setOriginalContent(draft.originalContent ?? "");
  }, [draft.id, draft.originalContent]);

  useEffect(() => {
    if (
      !expanded ||
      !isEdit ||
      draft.originalContent != null ||
      !draft.filePath ||
      !corpusRef
    ) {
      return;
    }

    let cancelled = false;

    void (async () => {
      setOriginalContentLoading(true);
      try {
        const committedContent = await readCommittedCorpusFile(
          corpusRef,
          draft.filePath,
        );
        if (!cancelled) {
          setOriginalContent(committedContent);
        }
      } catch {
        if (!cancelled) {
          setOriginalContent("");
        }
      } finally {
        if (!cancelled) {
          setOriginalContentLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [corpusRef, draft.filePath, draft.originalContent, expanded, isEdit]);

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
              <Button
                size="sm"
                variant="outline"
                className="h-6 px-2 text-xs"
                onClick={() => onPublish?.(draft.id)}
              >
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
            {isEdit ? (
              <ContextDiffPanel
                title={draft.title}
                subtitle="Draft changes"
                original={originalContent}
                modified={draft.content}
                path={`draft:${draft.id}`}
                loading={originalContentLoading}
              />
            ) : (
              <ContextDiffPanel
                title={draft.title}
                subtitle="New document"
                original=""
                modified={draft.content}
                path={`draft:${draft.id}:create`}
              />
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

function FileDetail({
  file,
  filePath,
  corpusRef,
  sessionToken,
  authorName,
  authorEmail,
  drafts,
  ensureDraftsLoaded,
  onDraftSaved,
  refreshDirtyPaths,
  onPushed,
}: {
  file: ContextFile;
  filePath: string;
  corpusRef: CorpusRepoRef | null;
  sessionToken?: string;
  authorName: string;
  authorEmail: string;
  drafts: DraftDocument[];
  ensureDraftsLoaded: () => Promise<DraftDocument[]>;
  onDraftSaved: (draft: DraftDocument) => void;
  refreshDirtyPaths: () => Promise<void>;
  onPushed: () => Promise<void>;
}) {
  const [viewMode, setViewMode] = useState<"published" | "draft" | "history">(
    "published",
  );
  const [editorContent, setEditorContent] = useState(file.content ?? "");
  const [committedContent, setCommittedContent] = useState(file.content ?? "");
  const [versions, setVersions] = useState<FileVersion[]>(file.versions);
  const [isFileLoading, setIsFileLoading] = useState(Boolean(corpusRef));
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [versionsLoaded, setVersionsLoaded] = useState(
    file.versions.length > 0,
  );
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isPushing, setIsPushing] = useState(false);
  const [isSavingDraft, setIsSavingDraft] = useState(false);
  const pendingWriteRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestContentRef = useRef(file.content ?? "");
  const committedContentRef = useRef(file.content ?? "");
  const existingDraft = useMemo(
    () =>
      drafts.find(
        (draft) => draft.status === "open" && draft.filePath === filePath,
      ) ?? null,
    [drafts, filePath],
  );

  const displayContent =
    viewMode === "draft" && file.draft?.content
      ? file.draft.content
      : editorContent;
  const displaySize =
    editorContent.length > 0 ? new Blob([editorContent]).size : file.size;
  const isDirty = editorContent !== committedContent;
  const canEditPublished = Boolean(corpusRef);
  const editorReadOnly =
    viewMode !== "published" || !canEditPublished || isPushing || isFileLoading;

  useEffect(() => {
    if (pendingWriteRef.current) {
      clearTimeout(pendingWriteRef.current);
      pendingWriteRef.current = null;
    }

    setViewMode("published");
    setVersions(file.versions);
    setVersionsLoaded(file.versions.length > 0);
    setVersionsLoading(false);
    setSaveError(null);
    setIsFileLoading(Boolean(corpusRef));

    let cancelled = false;

    const loadFile = async () => {
      if (!corpusRef) {
        const nextContent = file.content ?? "";
        const nextCommittedContent = file.versions[0]?.content ?? nextContent;
        if (cancelled) {
          return;
        }

        setEditorContent(nextContent);
        setCommittedContent(nextCommittedContent);
        latestContentRef.current = nextContent;
        committedContentRef.current = nextCommittedContent;
        setIsFileLoading(false);
        return;
      }

      try {
        const [nextContent, nextCommittedContent] = await Promise.all([
          readCorpusFile(corpusRef, filePath),
          readCommittedCorpusFile(corpusRef, filePath),
        ]);

        if (cancelled) {
          return;
        }

        setEditorContent(nextContent);
        setCommittedContent(nextCommittedContent);
        latestContentRef.current = nextContent;
        committedContentRef.current = nextCommittedContent;
      } catch (error) {
        if (cancelled) {
          return;
        }

        const message = error instanceof Error ? error.message : String(error);
        setSaveError(message);
        setEditorContent("");
        setCommittedContent("");
        latestContentRef.current = "";
        committedContentRef.current = "";
      } finally {
        if (!cancelled) {
          setIsFileLoading(false);
        }
      }
    };

    void loadFile();

    return () => {
      cancelled = true;
    };
  }, [corpusRef, file.content, file.versions, filePath]);

  useEffect(() => {
    if (viewMode !== "history" || versionsLoaded || versionsLoading) {
      return;
    }

    let cancelled = false;

    const loadVersions = async () => {
      if (!corpusRef) {
        setVersionsLoaded(true);
        return;
      }

      setVersionsLoading(true);
      try {
        const nextVersions = await readCorpusFileVersions(corpusRef, filePath);
        if (!cancelled) {
          setVersions(nextVersions);
          setVersionsLoaded(true);
        }
      } catch (error) {
        if (!cancelled) {
          const message =
            error instanceof Error ? error.message : String(error);
          setSaveError(message);
        }
      } finally {
        if (!cancelled) {
          setVersionsLoading(false);
        }
      }
    };

    void loadVersions();

    return () => {
      cancelled = true;
    };
  }, [corpusRef, filePath, versionsLoaded, versionsLoading, viewMode]);

  useEffect(() => {
    return () => {
      if (pendingWriteRef.current) {
        clearTimeout(pendingWriteRef.current);
      }
    };
  }, []);

  const persistWorkingCopy = useCallback(
    async (nextContent: string) => {
      if (!corpusRef) {
        return;
      }

      await writeCorpusFile(corpusRef, filePath, nextContent);
    },
    [corpusRef, filePath],
  );

  const flushPendingWrite = useCallback(async () => {
    if (!corpusRef || viewMode !== "published") {
      return;
    }

    if (pendingWriteRef.current) {
      clearTimeout(pendingWriteRef.current);
      pendingWriteRef.current = null;
    }

    await persistWorkingCopy(latestContentRef.current);
  }, [corpusRef, persistWorkingCopy, viewMode]);

  const handleEditorChange = useCallback(
    (nextContent: string) => {
      setEditorContent(nextContent);
      latestContentRef.current = nextContent;
      setSaveError(null);

      if (!corpusRef || viewMode !== "published") {
        return;
      }

      if (pendingWriteRef.current) {
        clearTimeout(pendingWriteRef.current);
      }

      pendingWriteRef.current = setTimeout(() => {
        void (async () => {
          try {
            await persistWorkingCopy(nextContent);
            await refreshDirtyPaths();
          } catch (error) {
            const message =
              error instanceof Error ? error.message : String(error);
            setSaveError(message);
          } finally {
            pendingWriteRef.current = null;
          }
        })();
      }, 250);
    },
    [corpusRef, persistWorkingCopy, refreshDirtyPaths, viewMode],
  );

  const handleDiscardChanges = useCallback(async () => {
    if (!corpusRef) {
      return;
    }

    if (pendingWriteRef.current) {
      clearTimeout(pendingWriteRef.current);
      pendingWriteRef.current = null;
    }

    setEditorContent(committedContent);
    latestContentRef.current = committedContent;
    setSaveError(null);

    try {
      await persistWorkingCopy(committedContent);
      await refreshDirtyPaths();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setSaveError(message);
    }
  }, [committedContent, corpusRef, persistWorkingCopy, refreshDirtyPaths]);

  const handlePushChanges = useCallback(async () => {
    if (!corpusRef) {
      return;
    }

    setIsPushing(true);
    setSaveError(null);

    try {
      await flushPendingWrite();
      await pushCorpusFile(corpusRef, filePath, {
        content: latestContentRef.current,
        token: sessionToken,
        authorName,
        authorEmail,
        message: `Update ${filePath}`,
      });
      setCommittedContent(latestContentRef.current);
      committedContentRef.current = latestContentRef.current;
      setVersions([]);
      setVersionsLoaded(false);
      await onPushed();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setSaveError(message);
    } finally {
      setIsPushing(false);
    }
  }, [
    authorEmail,
    authorName,
    corpusRef,
    filePath,
    flushPendingWrite,
    onPushed,
    refreshDirtyPaths,
    sessionToken,
  ]);

  const handlePushDraft = useCallback(async () => {
    if (!corpusRef) {
      return;
    }

    setIsSavingDraft(true);
    setSaveError(null);

    try {
      await flushPendingWrite();
      const openDrafts =
        existingDraft?.id != null ? drafts : await ensureDraftsLoaded();
      const matchingDraft =
        existingDraft ??
        openDrafts.find(
          (draft) => draft.status === "open" && draft.filePath === filePath,
        ) ??
        null;
      const savedDraft = await saveDraft("", {
        draftId: matchingDraft?.id,
        filePath,
        title: file.name,
        content: latestContentRef.current,
        originalContent: committedContentRef.current,
      });
      onDraftSaved(savedDraft);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setSaveError(message);
    } finally {
      setIsSavingDraft(false);
    }
  }, [
    corpusRef,
    drafts,
    ensureDraftsLoaded,
    existingDraft,
    file.name,
    filePath,
    flushPendingWrite,
    onDraftSaved,
  ]);

  const handleRestoreVersion = useCallback(
    async (version: FileVersion) => {
      if (!version.content || !corpusRef) {
        return;
      }

      if (pendingWriteRef.current) {
        clearTimeout(pendingWriteRef.current);
        pendingWriteRef.current = null;
      }

      setEditorContent(version.content);
      latestContentRef.current = version.content;
      setSaveError(null);
      setViewMode("published");

      try {
        await persistWorkingCopy(version.content);
        await refreshDirtyPaths();
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        setSaveError(message);
      }
    },
    [corpusRef, persistWorkingCopy, refreshDirtyPaths],
  );

  return (
    <div className="h-full min-h-0 rounded-lg border border-border bg-card overflow-hidden flex flex-col">
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
          <span>{formatFileSize(displaySize)}</span>
          <span>Updated {formatDate(file.updatedAt)}</span>
          <span className="font-mono">{filePath}</span>
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
            </>
          )}
          <LayerToggle
            active={viewMode === "history"}
            onClick={() => setViewMode("history")}
          >
            {versionsLoaded
              ? `${versions.length} Version${versions.length !== 1 ? "s" : ""}`
              : "Versions"}
          </LayerToggle>
        </div>
      </div>

      {viewMode === "history" && (
        <VersionHistory
          fileName={file.name}
          versions={versions}
          loading={versionsLoading}
          onRestoreVersion={handleRestoreVersion}
        />
      )}

      {viewMode === "published" && (
        <div className="flex-1 min-h-0 border-b border-border">
          {isFileLoading ? (
            <FileEditorSkeleton />
          ) : (
            <CorpusDiffEditor
              original={committedContent}
              modified={editorContent}
              path={`published:${filePath}`}
              className="h-full min-h-0"
              readOnly={editorReadOnly}
              onChange={handleEditorChange}
            />
          )}
        </div>
      )}

      {viewMode === "draft" && (
        <div className="flex-1 min-h-0 border-b border-border">
          {isFileLoading ? (
            <FileEditorSkeleton />
          ) : (
            <CorpusEditor
              value={displayContent}
              path={`${viewMode}:${filePath}`}
              className="h-full min-h-0"
              readOnly={editorReadOnly}
              onChange={handleEditorChange}
            />
          )}
        </div>
      )}

      <div className="shrink-0">
        <FeedbackPanel filePath={filePath} />
        <AnnotationsPanel filePath={filePath} />
      </div>

      <div className="shrink-0 px-4 py-3 border-t border-border flex items-center justify-between gap-3">
        <div className="min-w-0">
          {saveError ? (
            <Type small className="text-destructive">
              {saveError}
            </Type>
          ) : isFileLoading ? (
            <Type small muted>
              Loading file content...
            </Type>
          ) : viewMode !== "published" ? (
            <Type small muted>
              {viewMode === "draft"
                ? "Draft content is read-only here."
                : "History is read-only."}
            </Type>
          ) : !canEditPublished ? (
            <Type small muted>
              Corpus repo is not available for editing yet.
            </Type>
          ) : null}
        </div>

        {viewMode === "published" && (
          <div className="flex items-center gap-2 shrink-0">
            <Button
              size="sm"
              variant="outline"
              onClick={() => void handleDiscardChanges()}
              disabled={
                !canEditPublished || !isDirty || isPushing || isSavingDraft
              }
            >
              Discard
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void handlePushDraft()}
              disabled={
                !canEditPublished || !isDirty || isPushing || isSavingDraft
              }
            >
              {isSavingDraft ? "Pushing Draft..." : "Push Draft"}
            </Button>
            <Button
              size="sm"
              onClick={() => void handlePushChanges()}
              disabled={
                !canEditPublished || !isDirty || isPushing || isSavingDraft
              }
            >
              <GitCommitHorizontalIcon className="h-3.5 w-3.5 mr-1.5" />
              {isPushing ? "Pushing..." : "Push Live"}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function FileEditorSkeleton() {
  return (
    <div className="h-full min-h-0 p-4">
      <div className="h-full rounded-md border border-border bg-muted/20 p-4">
        <div className="space-y-3">
          {Array.from({ length: 16 }).map((_, index) => (
            <div key={index} className="flex items-center gap-4">
              <Skeleton className="h-4 w-6 shrink-0" />
              <Skeleton
                className={cn(
                  "h-4",
                  index % 6 === 0
                    ? "w-1/4"
                    : index % 4 === 0
                      ? "w-3/4"
                      : "w-1/2",
                )}
              />
            </div>
          ))}
        </div>
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

function VersionHistory({
  fileName,
  versions,
  loading,
  onRestoreVersion,
}: {
  fileName: string;
  versions: FileVersion[];
  loading: boolean;
  onRestoreVersion: (version: FileVersion) => Promise<void>;
}) {
  const [selectedVersion, setSelectedVersion] = useState<FileVersion | null>(
    null,
  );

  const previousVersion = useMemo(() => {
    if (!selectedVersion) return null;
    return (
      versions.find((v) => v.version === selectedVersion.version - 1) ?? null
    );
  }, [selectedVersion, versions]);

  const latestVersion = versions[0];

  useEffect(() => {
    if (versions.length === 0) {
      setSelectedVersion(null);
      return;
    }

    setSelectedVersion((current) => {
      if (!current) {
        return current;
      }

      return (
        versions.find((version) => version.version === current.version) ?? null
      );
    });
  }, [versions]);

  return (
    <div className="border-t border-border">
      {/* Version list */}
      <div className="max-h-[240px] overflow-y-auto">
        {loading && (
          <div className="px-4 py-4 space-y-3">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        )}
        {!loading &&
          versions.map((v) => {
            const isSelected = selectedVersion?.version === v.version;
            const prevV = versions.find((pv) => pv.version === v.version - 1);
            const wasRenamed = prevV?.path && v.path && prevV.path !== v.path;

            return (
              <div
                role="button"
                tabIndex={0}
                key={v.version}
                onClick={() => setSelectedVersion(isSelected ? null : v)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setSelectedVersion(isSelected ? null : v);
                  }
                }}
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
                    onClick={(e) => {
                      e.stopPropagation();
                      void onRestoreVersion(v);
                    }}
                    disabled={!v.content}
                  >
                    Restore
                  </Button>
                )}
              </div>
            );
          })}
        {!loading && versions.length === 0 && (
          <div className="px-4 py-6">
            <Type small muted>
              No published versions found for this file.
            </Type>
          </div>
        )}
      </div>

      {/* Selected version detail */}
      {selectedVersion && selectedVersion.content && (
        <div className="border-t border-border">
          <div className="flex items-center gap-1 px-4 py-2 border-b border-border bg-muted/20">
            <span className="text-xs font-medium text-muted-foreground mr-2">
              v{selectedVersion.version}
            </span>
            <span className="text-xs text-muted-foreground">
              {previousVersion
                ? `Diff from v${previousVersion.version}`
                : "Initial version"}
            </span>
          </div>
          <ContextDiffPanel
            title={fileName}
            subtitle={
              previousVersion
                ? `Diff from v${previousVersion.version}`
                : "Initial version"
            }
            original={previousVersion?.content ?? ""}
            modified={selectedVersion.content}
            path={
              previousVersion
                ? `history:${fileName}:v${previousVersion.version}-v${selectedVersion.version}`
                : `history:${fileName}:initial-v${selectedVersion.version}`
            }
          />
        </div>
      )}
    </div>
  );
}

function ContextDiffPanel({
  title,
  subtitle,
  original,
  modified,
  path,
  loading = false,
}: {
  title: string;
  subtitle: string;
  original: string;
  modified: string;
  path: string;
  loading?: boolean;
}) {
  return (
    <div className="rounded-md border border-border overflow-hidden bg-muted/10">
      <div className="flex items-center justify-between gap-3 px-4 py-2 border-b border-border bg-muted/20">
        <div className="min-w-0">
          <Type small className="font-medium truncate">
            {title}
          </Type>
          <Type small muted className="truncate">
            {subtitle}
          </Type>
        </div>
      </div>
      <div className="p-4">
        {loading ? (
          <div className="h-[400px] rounded-md border border-border bg-card p-4 space-y-3">
            {Array.from({ length: 12 }).map((_, index) => (
              <Skeleton
                key={index}
                className={cn(
                  "h-4",
                  index % 4 === 0
                    ? "w-2/3"
                    : index % 3 === 0
                      ? "w-5/6"
                      : "w-1/2",
                )}
              />
            ))}
          </div>
        ) : (
          <CorpusDiffEditor
            original={original}
            modified={modified}
            path={path}
            height="400px"
            readOnly
          />
        )}
      </div>
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
  corpusRemoteURL,
  projectSlug,
}: {
  folder: ContextFolder;
  path: string[];
  config: DocsMcpConfig | null;
  corpusRemoteURL: string | null;
  projectSlug: string | null;
}) {
  const counts = countItems(folder);
  const drafts = countDrafts(folder);
  const localConfigFile = findFile(folder, ".docs-mcp.json");
  const isRootFolder = path.length === 0;
  const quickStartCommand = corpusRemoteURL
    ? buildCorpusQuickStart(corpusRemoteURL, projectSlug)
    : null;

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <Icon name="folder" className="h-4 w-4 text-muted-foreground" />
          <Type variant="subheading">
            {path.length === 0 ? "Repository" : path[path.length - 1]}
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

      {isRootFolder && corpusRemoteURL && (
        <div className="p-4 border-b border-border space-y-3">
          <div className="space-y-1">
            <Type small muted className="font-medium block">
              Git Remote
            </Type>
            <Type small muted>
              Clone and push directly against the repo backing this context.
            </Type>
          </div>

          <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-xs text-foreground">
                {corpusRemoteURL}
              </code>
              <CopyButton
                text={corpusRemoteURL}
                size="icon-sm"
                tooltip="Copy remote URL"
              />
            </div>
          </div>

          {quickStartCommand && (
            <div className="rounded-md border border-border bg-muted/20 p-3 space-y-2">
              <div className="flex items-center justify-between gap-2">
                <Type small muted className="font-medium">
                  Quick Start
                </Type>
                <CopyButton
                  text={quickStartCommand}
                  size="icon-sm"
                  tooltip="Copy quick start"
                />
              </div>
              <pre className="overflow-x-auto whitespace-pre-wrap text-xs font-mono text-foreground">
                {quickStartCommand}
              </pre>
            </div>
          )}
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
