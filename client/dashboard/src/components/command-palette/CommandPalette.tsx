import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Kbd } from "@/components/ui/kbd";
import { BrandGradientLine } from "@/components/brand-gradient-line";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useSlugs } from "@/contexts/Sdk";
import { Badge } from "@/components/ui/badge";
import { DynamicIcon, type IconName } from "@/components/ui/dynamic-icon";
import { Sparkles } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { requestAskAi } from "./askAiBridge";
import { useRecentlyVisited, useRecentsUserId } from "./recentlyVisited";
import { ResourceResults } from "./ResourceResults";

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
  // session lookup on `isOpen` so we don't poll auth.info on every page (it
  // 401s when unauthenticated); gate the read on the user id resolving so we
  // never read the shared anonymous key before the session loads.
  const recentsUserId = useRecentsUserId(isOpen);
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
  // Recently Visited is a zero-state convenience and the Project Assistant row
  // moves to the bottom once the user starts typing (see below), so both branch
  // on whether there's an active query.
  const hasQuery = trimmedQuery.length > 0;
  const askAiLabel = trimmedQuery
    ? `Ask AI: "${trimmedQuery}"`
    : "Ask the Project Assistant…";

  const handleAskAi = () => {
    requestAskAi(trimmedQuery);
    closeAndReset();
  };

  // Free-form AI escape hatch — always offered regardless of the filter
  // (forceMount) so the typed query can always be sent to the assistant.
  // Project Assistant is project-scoped, so only at the project level. Rendered
  // near the top when the palette is idle (discoverable) but pushed below the
  // results while searching: cmdk auto-selects the first item in DOM order after
  // filtering, so keeping this forceMounted row above the matches would steal
  // the highlight from the closest result and force an extra ↓ keypress to reach
  // it (AGE-2807).
  const askAiGroup = inProject ? (
    <CommandGroup heading="Assistant">
      <CommandItem
        forceMount
        value="__ask_ai__"
        onSelect={handleAskAi}
        className="flex items-center gap-2"
      >
        <Sparkles className="text-primary size-4 shrink-0" />
        <span className="truncate">{askAiLabel}</span>
      </CommandItem>
    </CommandGroup>
  ) : null;

  return (
    <CommandDialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open) closeAndReset();
      }}
    >
      <BrandGradientLine className="h-0.5" />
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

        {/* Recently visited pages (most-recent first), client-side localStorage.
            Only a zero-state affordance: once the user types, hide it so search
            results rank on their own merits instead of recents jumping ahead of
            a closer text match (AGE-2808). */}
        {!hasQuery && recents.length > 0 && (
          <CommandGroup heading="Recently Visited">
            {recents.map((recent) => (
              <CommandItem
                key={recent.href}
                value={`recent ${recent.label} ${recent.href}`}
                onSelect={() => handleRecentSelect(recent.href)}
                className="flex items-center gap-2"
              >
                {recent.icon && (
                  <DynamicIcon
                    name={recent.icon as IconName}
                    className="size-4"
                  />
                )}
                <span className="truncate">{recent.label}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* Idle: Ask AI sits up top for discoverability. */}
        {!hasQuery && askAiGroup}

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
                    <DynamicIcon
                      name={action.icon as IconName}
                      className="size-4"
                    />
                  )}
                  <span>{action.label}</span>
                  {action.stage && (
                    <ReleaseStageBadge stage={action.stage} noTooltip />
                  )}
                </div>
                {action.shortcut && <Kbd>{action.shortcut}</Kbd>}
              </CommandItem>
            ))}
          </CommandGroup>
        ))}

        {/* Resource search results — only mounted while open, so the list
            fetches lazily on first open (React Query caches thereafter). */}
        {isOpen && inProject && (
          <ResourceResults onNavigate={closeAndReset} query={trimmedQuery} />
        )}

        {/* Searching: Ask AI drops below the results so the closest match keeps
            the auto-selected highlight, while the "Ask AI: …" fallback stays
            available at the bottom of the list (AGE-2807). */}
        {hasQuery && askAiGroup}
      </CommandList>

      {/* Keyboard navigation hints */}
      <div className="text-muted-foreground flex items-center gap-3 border-t px-3 py-2 text-xs">
        <span className="flex items-center gap-1.5">
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd>
          to navigate
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>↵</Kbd>
          to select
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd>esc</Kbd>
          to close
        </span>
      </div>
    </CommandDialog>
  );
}
