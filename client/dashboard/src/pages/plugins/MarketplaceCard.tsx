import { Book, ExternalLink, RefreshCw, Settings, Users } from "lucide-react";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Badge } from "@/components/ui/Badge";

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
    <div className="border-border bg-card border p-4">
      <div>
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
            className="min-w-0 truncate text-base text-link-primary hover:underline"
          >
            {publishStatus.repoOwner}
            {publishStatus.repoOwner && publishStatus.repoName && (
              <span className="text-muted-foreground/60 mx-1">/</span>
            )}
            <span className="font-semibold">{publishStatus.repoName}</span>
            <ExternalLink className="ml-1 inline h-3.5 w-3.5 align-text-top" />
          </a>
          <span className="border-border text-muted-foreground border px-2 py-0 font-mono text-[10px] font-medium tracking-wider uppercase">
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
                    ? "absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-warning-default opacity-40"
                    : "absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-success-default opacity-40"
                }
              />
              <span
                className={
                  hasUnpublishedChanges
                    ? "relative inline-flex h-2.5 w-2.5 rounded-full bg-warning-default"
                    : "relative inline-flex h-2.5 w-2.5 rounded-full bg-success-default"
                }
              />
            </span>
            <span
              className={
                hasUnpublishedChanges
                  ? "font-medium text-default-warning"
                  : "font-medium text-default-success"
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
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50"
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
              className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors"
            >
              <ExternalLink className="h-4 w-4" />
              Open
            </a>
            <button
              type="button"
              onClick={onManageCollaborators}
              className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors"
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
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors"
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
// — same shell, warning-toned status instead of the connected state's
// success tone. Whether a repo already exists (repoUrl present)
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
    <div className="border-border bg-card border p-4">
      <div>
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
              className="min-w-0 truncate text-base text-link-primary hover:underline"
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
              <span className="absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-warning-default opacity-40" />
              <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-warning-default" />
            </span>
            <span className="font-medium text-default-warning">
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
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors"
              >
                <Users className="h-4 w-4" />
                Add collaborators
              </button>
            ) : (
              <button
                type="button"
                onClick={onSetup}
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors"
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
