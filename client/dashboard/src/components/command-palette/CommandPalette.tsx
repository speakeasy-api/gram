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

export function CommandPalette() {
  const { isOpen, close, actions, contextBadge } = useCommandPalette();

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
    close();
  };

  return (
    <CommandDialog open={isOpen} onOpenChange={close}>
      {contextBadge && (
        <div className="px-3 pt-3 pb-2">
          <Badge variant="neutral">
            <Badge.Text>{contextBadge}</Badge.Text>
          </Badge>
        </div>
      )}
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
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
                  <span className="text-xs text-muted-foreground">
                    {action.shortcut}
                  </span>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
        ))}
      </CommandList>
    </CommandDialog>
  );
}
