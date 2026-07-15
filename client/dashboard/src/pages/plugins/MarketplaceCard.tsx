import { Book, ExternalLink, RefreshCw, Settings, Users } from "lucide-react";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Badge } from "@/components/ui/badge";

// The connected-state marketplace card, shared verbatim across the plugins
// list, plugin detail page, and the onboarding setup wizard
// (create-marketplace-step.tsx) so the three surfaces never drift.
export function MarketplaceCard({
  publishStatus,
  onManageCollaborators,
  onRename,
  onSync,
  isSyncing = false,
  description = "This repo is your team's plugin marketplace. The observability plugins are already inside, and any plugins you build in Speakeasy later will be published here too.",
}: {
  publishStatus: PublishStatusResult;
  onManageCollaborators: () => void;
  onRename?: () => void;
  /** Republishes the marketplace to pick up unpublished plugin edits. */
  onSync?: () => void;
  isSyncing?: boolean;
  description?: string;
}): JSX.Element {
  // upToDate is undefined/null when freshness can't be determined (a
  // connection that predates fingerprinting) — only the explicit `false`
  // case is a known, real drift worth warning about.
  const hasUnpublishedChanges = publishStatus.upToDate === false;

  return (
    <div className="border-border relative overflow-hidden rounded-md border p-4">
      <div
        aria-hidden="true"
        className={
          hasUnpublishedChanges
            ? "from-amber-50/10 via-amber-50 to-amber-100/50 dark:from-amber-950/40 dark:via-neutral-800 dark:to-amber-900/20 absolute inset-0 bg-gradient-to-br"
            : "from-slate-50/10 via-slate-50 to-emerald-100/50 dark:from-slate-950/60 dark:via-neutral-800 dark:to-emerald-900/30 absolute inset-0 bg-gradient-to-br"
        }
      />
      <div className="relative">
        <div className="mb-1.5 flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs font-medium tracking-wide uppercase">
            Your project marketplace
          </span>
          {hasUnpublishedChanges ? (
            <Badge variant="warning">
              <Badge.Text>Needs syncing</Badge.Text>
            </Badge>
          ) : (
            <Badge variant="success">
              <Badge.Text>Up to date</Badge.Text>
            </Badge>
          )}
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Book className="text-muted-foreground h-4 w-4 flex-shrink-0" />
          <a
            href={publishStatus.repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="min-w-0 truncate text-base text-sky-500 hover:text-sky-600 hover:underline"
          >
            {publishStatus.repoOwner}
            {publishStatus.repoOwner && publishStatus.repoName && (
              <span className="text-muted-foreground/60 mx-1">/</span>
            )}
            <span className="font-semibold">{publishStatus.repoName}</span>
            <ExternalLink className="ml-1 inline h-3.5 w-3.5 align-text-top" />
          </a>
          <span className="border-border text-muted-foreground rounded-full border px-2 py-0 text-[10px] font-medium tracking-wider uppercase">
            Private
          </span>
        </div>
        <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
          {description}
        </p>
        <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
          At least one member of your GitHub organization{" "}
          <strong className="text-foreground font-semibold">
            must be added as a collaborator
          </strong>{" "}
          to the marketplace repository so that the repository is discoverable
          inside of Claude, Codex and other platforms when adding the plugin
          repository.
        </p>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <span className="text-muted-foreground inline-flex items-center gap-1.5 text-sm">
            <span className="relative flex h-2.5 w-2.5">
              <span
                className={
                  hasUnpublishedChanges
                    ? "absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-amber-500 opacity-40"
                    : "absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-emerald-500 opacity-40"
                }
              />
              <span
                className={
                  hasUnpublishedChanges
                    ? "relative inline-flex h-2.5 w-2.5 rounded-full bg-amber-500"
                    : "relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500"
                }
              />
            </span>
            <span
              className={
                hasUnpublishedChanges
                  ? "font-medium text-amber-700 dark:text-amber-400"
                  : "font-medium text-emerald-700 dark:text-emerald-400"
              }
            >
              {hasUnpublishedChanges
                ? "Not up to date. Click 'Sync changes' to publish"
                : "Marketplace set up"}
            </span>
          </span>
          <div className="flex flex-wrap items-center gap-2">
            {hasUnpublishedChanges && onSync && (
              <button
                type="button"
                onClick={onSync}
                disabled={isSyncing}
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50"
              >
                <RefreshCw
                  className={isSyncing ? "h-4 w-4 animate-spin" : "h-4 w-4"}
                />
                {isSyncing ? "Syncing..." : "Sync changes"}
              </button>
            )}
            <a
              href={publishStatus.repoUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors"
            >
              <ExternalLink className="h-4 w-4" />
              Open
            </a>
            <button
              type="button"
              onClick={onManageCollaborators}
              className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors"
            >
              <Users className="h-4 w-4" />
              Manage collaborators
            </button>
            {onRename && (
              <button
                type="button"
                onClick={onRename}
                aria-label="Rename marketplace"
                title="Rename marketplace"
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors"
              >
                <Settings className="h-4 w-4" />
                Rename
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// The not-yet-connected / no-collaborators-yet counterpart to MarketplaceCard
// — same shell and gradient treatment, warning-toned instead of the
// connected state's emerald. Whether a repo already exists (repoUrl present)
// distinguishes the two incomplete states: no repo yet ("Setup", which picks
// a name then publishes) vs. a repo that exists but has no directly-added
// collaborator yet ("Add collaborators", which skips straight to that
// dialog) — the repo link itself is only a skeleton in the former case,
// since there's no real URL to show until the repo is created.
export function UninitializedMarketplaceCard({
  publishStatus,
  defaultName,
  onSetup,
  onAddCollaborators,
  description = "This repo will be your team's plugin marketplace. The observability plugins will already be inside, and any plugins you build in Speakeasy later will be published here too.",
}: {
  publishStatus: Pick<
    PublishStatusResult,
    "repoOwner" | "repoName" | "repoUrl"
  >;
  defaultName?: string;
  onSetup: () => void;
  onAddCollaborators: () => void;
  description?: string;
}): JSX.Element {
  const hasRepo = !!publishStatus.repoUrl;

  return (
    <div className="border-border relative overflow-hidden rounded-md border p-4">
      <div
        aria-hidden="true"
        className="from-amber-50/10 via-amber-50 to-amber-100/50 dark:from-amber-950/40 dark:via-neutral-800 dark:to-amber-900/20 absolute inset-0 bg-gradient-to-br"
      />
      <div className="relative">
        <div className="mb-1.5 flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs font-medium tracking-wide uppercase">
            Your project marketplace
          </span>
          <Badge variant="warning">
            <Badge.Text>Not published</Badge.Text>
          </Badge>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Book className="text-muted-foreground h-4 w-4 flex-shrink-0" />
          {hasRepo ? (
            <a
              href={publishStatus.repoUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="min-w-0 truncate text-base text-sky-500 hover:text-sky-600 hover:underline"
            >
              {publishStatus.repoOwner}
              {publishStatus.repoOwner && publishStatus.repoName && (
                <span className="text-muted-foreground/60 mx-1">/</span>
              )}
              <span className="font-semibold">{publishStatus.repoName}</span>
              <ExternalLink className="ml-1 inline h-3.5 w-3.5 align-text-top" />
            </a>
          ) : (
            <span className="text-muted-foreground min-w-0 truncate text-base">
              {defaultName}
            </span>
          )}
        </div>
        <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
          {description}
        </p>
        <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
          At least one member of your GitHub organization{" "}
          <strong className="text-foreground font-semibold">
            must be added as a collaborator
          </strong>{" "}
          to the marketplace repository so that the repository is discoverable
          inside of Claude, Codex and other platforms when adding the plugin
          repository.
        </p>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <span className="text-muted-foreground inline-flex items-start gap-2 text-sm">
            <span className="relative mt-1 flex h-2.5 w-2.5 shrink-0">
              <span className="absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-amber-500 opacity-40" />
              <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-amber-500" />
            </span>
            <span className="font-medium text-amber-700 dark:text-amber-400">
              {hasRepo ? (
                <>
                  Marketplace doesn't have any active collaborators added.
                  <br />
                  Please ensure you have accepted the invite email.
                </>
              ) : (
                "Marketplace not yet setup"
              )}
            </span>
          </span>
          <div className="flex flex-wrap items-center gap-2">
            {hasRepo ? (
              <button
                type="button"
                onClick={onAddCollaborators}
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors"
              >
                <Users className="h-4 w-4" />
                Add collaborators
              </button>
            ) : (
              <button
                type="button"
                onClick={onSetup}
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 rounded-md border px-3.5 py-2 text-sm font-medium transition-colors"
              >
                <Settings className="h-4 w-4" />
                Publish now
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
