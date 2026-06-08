import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { Icon, IconName, Badge } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { requestAskAi } from "./askAiBridge";
import { ResourceResults } from "./ResourceResults";

export function CommandPalette(): JSX.Element {
  const { isOpen, close, actions, contextBadge } = useCommandPalette();
  const [query, setQuery] = useState("");

  const closeAndReset = () => {
    setQuery("");
    close();
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

  // Sort groups: Tool Actions first (when present), then others alphabetically
  const sortedGroups = Object.entries(groupedActions).sort(([a], [b]) => {
    if (a === "Tool Actions") return -1;
    if (b === "Tool Actions") return 1;
    return a.localeCompare(b);
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
      {contextBadge && (
        <div className="px-3 pt-3 pb-2">
          <Badge variant="neutral">
            <Badge.Text>{contextBadge}</Badge.Text>
          </Badge>
        </div>
      )}
      <CommandInput
        placeholder="Ask AI or search resources and pages…"
        value={query}
        onValueChange={setQuery}
      />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        {/* Free-form AI escape hatch — always offered regardless of the filter
            (forceMount) so the typed query can always be sent to the assistant. */}
        <CommandGroup heading="Assistant">
          <CommandItem
            forceMount
            value="__ask_ai__"
            onSelect={handleAskAi}
            className="flex items-center gap-2"
          >
            <Icon name="sparkles" className="size-4 shrink-0" />
            <span className="truncate">{askAiLabel}</span>
          </CommandItem>
        </CommandGroup>

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
        {isOpen && <ResourceResults onNavigate={closeAndReset} />}
      </CommandList>
    </CommandDialog>
  );
}
