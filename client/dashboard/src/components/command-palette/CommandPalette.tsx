import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useSlugs } from "@/contexts/Sdk";
import { Icon, IconName, Badge } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { useNavigate } from "react-router";
import { requestAskAi } from "./askAiBridge";
import { useRecentlyVisited, useRecentsUserId } from "./recentlyVisited";
import { ResourceResults } from "./ResourceResults";

// Speakeasy brand spectrum — the same brand-language gradient the Project
// Assistant uses. Rendered as a thin hairline at the top of the palette so the
// surface reads as Gram without leaning on display type or heavy chrome.
const BRAND_GRADIENT =
  "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)";

const KBD_CLASS =
  "border-neutral-softest bg-muted text-muted-foreground pointer-events-none inline-flex h-5 min-w-5 items-center justify-center gap-1 rounded border px-1.5 font-mono text-[10px] font-medium select-none";

export function CommandPalette(): JSX.Element {
  const { isOpen, close, actions, contextBadge } = useCommandPalette();
  const { orgSlug, projectSlug } = useSlugs();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");

  // Project Assistant and resource search are project-scoped. At the org level
  // (no project in the URL) the palette still works for navigating org pages.
  const inProject = Boolean(projectSlug);

  // Recently visited pages (client-side; read only while the palette is open).
  // Scoped per user so a shared browser profile doesn't leak history. Gate the
  // read on the user id resolving so we never read the shared anonymous key
  // before the session loads.
  const recentsUserId = useRecentsUserId();
  const recents = useRecentlyVisited(
    recentsUserId,
    orgSlug,
    projectSlug,
    isOpen && Boolean(recentsUserId),
  );

  const closeAndReset = () => {
    setQuery("");
    close();
  };

  const handleRecentSelect = (href: string) => {
    void navigate(href);
    closeAndReset();
  };

  // Group actions by their group property
  const groupedActions = actions.reduce(
    (acc, action) => {
      const group = action.group || "Actions";
      if (!acc[group]) {
        acc[group] = [];
      }
      acc[group].push(action);
      return acc;
    },
    {} as Record<string, typeof actions>,
  );

  // Sort groups by an explicit priority: contextual Tool Actions first, then
  // project Pages before Organization pages, then anything else alphabetically.
  const groupPriority = (name: string): number => {
    switch (name) {
      case "Tool Actions":
        return 0;
      case "Pages":
        return 1;
      case "Organization":
        return 2;
      default:
        return 3;
    }
  };
  const sortedGroups = Object.entries(groupedActions).sort(([a], [b]) => {
    const byPriority = groupPriority(a) - groupPriority(b);
    return byPriority !== 0 ? byPriority : a.localeCompare(b);
  });

  const handleSelect = (action: (typeof actions)[0]) => {
    action.onSelect();
    closeAndReset();
  };

  const trimmedQuery = query.trim();
  const askAiLabel = trimmedQuery
    ? `Ask AI: "${trimmedQuery}"`
    : "Ask the Project Assistant…";

  const handleAskAi = () => {
    requestAskAi(trimmedQuery);
    closeAndReset();
  };

  return (
    <CommandDialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open) closeAndReset();
      }}
    >
      {/* Speakeasy brand hairline */}
      <div
        aria-hidden
        className="h-0.5 w-full shrink-0"
        style={{ background: BRAND_GRADIENT }}
      />
      {contextBadge && (
        <div className="px-3 pt-3 pb-2">
          <Badge variant="neutral">
            <Badge.Text>{contextBadge}</Badge.Text>
          </Badge>
        </div>
      )}
      <CommandInput
        placeholder={
          inProject ? "Ask AI or search resources and pages…" : "Search pages…"
        }
        value={query}
        onValueChange={setQuery}
        onKeyDown={(e) => {
          // Two-step Escape: first clears the query, then (when already empty)
          // bubbles up to close the palette.
          if (e.key === "Escape" && query) {
            e.preventDefault();
            e.stopPropagation();
            setQuery("");
          }
        }}
      />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        {/* Recently visited pages (most-recent first), client-side localStorage. */}
        {recents.length > 0 && (
          <CommandGroup heading="Recently Visited">
            {recents.map((recent) => (
              <CommandItem
                key={recent.href}
                value={`recent ${recent.label} ${recent.href}`}
                onSelect={() => handleRecentSelect(recent.href)}
                className="flex items-center gap-2"
              >
                {recent.icon && (
                  <Icon name={recent.icon as IconName} className="size-4" />
                )}
                <span className="truncate">{recent.label}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* Free-form AI escape hatch — always offered regardless of the filter
            (forceMount) so the typed query can always be sent to the assistant.
            Project Assistant is project-scoped, so only at the project level. */}
        {inProject && (
          <CommandGroup heading="Assistant">
            <CommandItem
              forceMount
              value="__ask_ai__"
              onSelect={handleAskAi}
              className="flex items-center gap-2"
            >
              <Icon name="sparkles" className="text-primary size-4 shrink-0" />
              <span className="truncate">{askAiLabel}</span>
            </CommandItem>
          </CommandGroup>
        )}

        {sortedGroups.map(([groupName, groupActions]) => (
          <CommandGroup key={groupName} heading={groupName}>
            {groupActions.map((action) => (
              <CommandItem
                key={action.id}
                onSelect={() => handleSelect(action)}
                className="flex items-center justify-between"
              >
                <div className="flex items-center gap-2">
                  {action.icon && (
                    <Icon name={action.icon as IconName} className="size-4" />
                  )}
                  <span>{action.label}</span>
                  {action.stage && (
                    <ReleaseStageBadge stage={action.stage} noTooltip />
                  )}
                </div>
                {action.shortcut && (
                  <span className="text-muted-foreground text-xs">
                    {action.shortcut}
                  </span>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
        ))}

        {/* Resource search results — only mounted while open, so the list
            fetches lazily on first open (React Query caches thereafter). */}
        {isOpen && inProject && (
          <ResourceResults onNavigate={closeAndReset} query={trimmedQuery} />
        )}
      </CommandList>

      {/* Keyboard navigation hints */}
      <div className="text-muted-foreground flex items-center gap-3 border-t px-3 py-2 text-xs">
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>↑</kbd>
          <kbd className={KBD_CLASS}>↓</kbd>
          to navigate
        </span>
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>↵</kbd>
          to select
        </span>
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>esc</kbd>
          to close
        </span>
      </div>
    </CommandDialog>
  );
}
