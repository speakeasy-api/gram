import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import {
  Book,
  ChevronDown,
  ExternalLink,
  RefreshCw,
  Settings,
  Users,
} from "lucide-react";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Badge } from "@/components/ui/Badge";

// The connected-state marketplace card, shared verbatim across the plugins
// list and plugin detail page so the two surfaces never drift.
export function MarketplaceCard({
  publishStatus,
  onManageCollaborators,
  onRename,
  onSync,
  isSyncing = false,
  observabilityEnabled = true,
  pausedPluginNames = [],
}: {
  publishStatus: PublishStatusResult;
  onManageCollaborators?: () => void;
  onRename?: () => void;
  /** Republishes the marketplace to pick up unpublished plugin edits. */
  onSync?: () => void;
  isSyncing?: boolean;
  /** Drives the copy: a disabled project's marketplace has no observability plugins in it. */
  observabilityEnabled?: boolean;
  /** Plugins whose auto sync is off: their changes publish only on Sync changes. */
  pausedPluginNames?: string[];
}): JSX.Element {
  const description = observabilityEnabled
    ? "This repo is your team's plugin marketplace. The observability plugins are already inside, and any plugins you build in Speakeasy later will be published here too."
    : "This repo is your team's plugin marketplace. Any plugins you build in Speakeasy are published here; the observability plugins are disabled for this project.";
  const freshnessKnown = typeof publishStatus.upToDate === "boolean";
  const isCurrent = publishStatus.upToDate === true;
  const hasUnpublishedChanges = publishStatus.upToDate === false;
  // False when the only pending changes belong to plugins whose auto sync is
  // off: no automatic publish will send them.
  const changesSyncAutomatically = publishStatus.automaticSyncPending !== false;
  const statusDot = isCurrent
    ? "bg-success-default"
    : hasUnpublishedChanges
      ? "bg-warning-default"
      : "bg-muted-foreground";
  const statusTextColor = isCurrent
    ? "text-default-success"
    : hasUnpublishedChanges
      ? "text-default-warning"
      : "text-muted-foreground";
  let statusMessage = "Marketplace set up";
  if (!freshnessKnown) {
    statusMessage =
      "Sync status is unavailable. Check again shortly or sync changes.";
  } else if (hasUnpublishedChanges) {
    statusMessage = changesSyncAutomatically
      ? "Not up to date yet. Changes sync automatically, or click 'Sync changes' to publish now"
      : "Not up to date. The waiting changes belong to plugins with auto sync off, so they publish only when you click 'Sync changes'";
  }

  return (
    <Collapsible
      defaultOpen={false}
      className="border-border bg-card border p-4"
    >
      <div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs font-medium tracking-wide uppercase">
            Your project marketplace
          </span>
          <Badge
            variant={
              hasUnpublishedChanges
                ? "warning"
                : isCurrent
                  ? "success"
                  : "neutral"
            }
          >
            <Badge.Text>
              {hasUnpublishedChanges
                ? "Needs syncing"
                : isCurrent
                  ? "Up to date"
                  : "Sync status unavailable"}
            </Badge.Text>
          </Badge>
          <CollapsibleTrigger
            aria-label="Toggle project marketplace details"
            className="text-muted-foreground hover:text-foreground focus-visible:ring-ring ml-auto flex size-8 shrink-0 items-center justify-center focus-visible:ring-2 focus-visible:outline-none [&[data-state=open]>svg]:rotate-180"
          >
            <ChevronDown
              className="size-4 transition-transform"
              aria-hidden="true"
            />
          </CollapsibleTrigger>
        </div>
        <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-2">
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
        <CollapsibleContent>
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
            repository. Collaborators are added as repository admins, which
            Cursor requires to serve this marketplace to your team.
          </p>
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
            <span className="text-muted-foreground inline-flex items-center gap-1.5 text-sm">
              <span className="relative flex h-2.5 w-2.5">
                {freshnessKnown && (
                  <span
                    className={`absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full opacity-40 ${statusDot}`}
                  />
                )}
                <span
                  className={`relative inline-flex h-2.5 w-2.5 rounded-full ${statusDot}`}
                />
              </span>
              <span className={`font-medium ${statusTextColor}`}>
                {statusMessage}
              </span>
            </span>
            <div className="flex flex-wrap items-center gap-2">
              {!isCurrent && onSync && (
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
                disabled={!onManageCollaborators}
                className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50"
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
          {pausedPluginNames.length > 0 && (
            <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
              {pausedPluginsNote(pausedPluginNames)}
            </p>
          )}
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}

// pausedPluginsNote names the plugins whose auto sync is off, so the admin knows
// the "changes sync automatically" promise above does not cover them.
function pausedPluginsNote(names: string[]): string {
  const listed =
    names.length <= 3
      ? new Intl.ListFormat("en", {
          style: "long",
          type: "conjunction",
        }).format(names)
      : `${names.length} plugins`;
  const pronoun = names.length === 1 ? "its" : "their";
  return `Auto sync is off for ${listed}, so ${pronoun} changes publish only when someone clicks Sync changes.`;
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
  observabilityEnabled = true,
}: {
  publishStatus: Pick<
    PublishStatusResult,
    "repoOwner" | "repoName" | "repoUrl"
  >;
  defaultName?: string;
  onSetup?: () => void;
  onAddCollaborators?: () => void;
  /** Drives the copy: a disabled project's marketplace has no observability plugins in it. */
  observabilityEnabled?: boolean;
}): JSX.Element {
  const description = observabilityEnabled
    ? "This repo will be your team's plugin marketplace. The observability plugins will already be inside, and any plugins you build in Speakeasy later will be published here too."
    : "This repo will be your team's plugin marketplace. Any plugins you build in Speakeasy will be published here; the observability plugins are disabled for this project.";
  const hasRepo = !!publishStatus.repoUrl;

  return (
    <Collapsible defaultOpen className="border-border bg-card border p-4">
      <div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs font-medium tracking-wide uppercase">
            Your project marketplace
          </span>
          <Badge variant="warning">
            <Badge.Text>Not published</Badge.Text>
          </Badge>
          <CollapsibleTrigger
            aria-label="Toggle project marketplace details"
            className="text-muted-foreground hover:text-foreground focus-visible:ring-ring ml-auto flex size-8 shrink-0 items-center justify-center focus-visible:ring-2 focus-visible:outline-none [&[data-state=open]>svg]:rotate-180"
          >
            <ChevronDown
              className="size-4 transition-transform"
              aria-hidden="true"
            />
          </CollapsibleTrigger>
        </div>
        <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-2">
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
        <CollapsibleContent>
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
            repository. Collaborators are added as repository admins, which
            Cursor requires to serve this marketplace to your team.
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
                  disabled={!onAddCollaborators}
                  className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                >
                  <Users className="h-4 w-4" />
                  Add collaborators
                </button>
              ) : (
                <button
                  type="button"
                  onClick={onSetup}
                  disabled={!onSetup}
                  className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-2 border px-3.5 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                >
                  <Settings className="h-4 w-4" />
                  Publish now
                </button>
              )}
            </div>
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}
