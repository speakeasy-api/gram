import { Book, ExternalLink, Settings, Users } from "lucide-react";
import type { PublishStatusResult } from "@gram/client/models/components";

// The connected-state marketplace card, shared verbatim across the plugins
// list, plugin detail page, and the onboarding setup wizard
// (create-marketplace-step.tsx) so the three surfaces never drift.
export function MarketplaceCard({
  publishStatus,
  onManageCollaborators,
  onRename,
  description = "This repo is your team's plugin marketplace. The observability plugins are already inside, and any plugins you build in Speakeasy later will be published here too.",
}: {
  publishStatus: PublishStatusResult;
  onManageCollaborators: () => void;
  onRename?: () => void;
  description?: string;
}): JSX.Element {
  return (
    <div className="border-border relative overflow-hidden rounded-md border p-4">
      <div
        aria-hidden="true"
        className="from-slate-50/10 via-slate-50 to-emerald-100/50 dark:from-slate-950/60 dark:via-neutral-800 dark:to-emerald-900/30 absolute inset-0 bg-gradient-to-br"
      />
      <div className="relative">
        <span className="text-muted-foreground mb-1.5 block font-mono text-xs font-medium tracking-wide uppercase">
          Your project marketplace
        </span>
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
              <span className="absolute inline-flex h-full w-full motion-safe:animate-ping rounded-full bg-emerald-500 opacity-40" />
              <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500" />
            </span>
            <span className="font-medium text-emerald-700 dark:text-emerald-400">
              Marketplace set up
            </span>
          </span>
          <div className="flex flex-wrap items-center gap-2">
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
