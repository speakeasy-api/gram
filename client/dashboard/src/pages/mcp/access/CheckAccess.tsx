import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Text } from "@/components/ui/Text";
import { useMembers } from "@gram/client/react-query/members.js";
import { Search, X } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { CheckAccessResult } from "./CheckAccessResult";

/**
 * Pick one person and see whether they can use this server, and why. The
 * answer comes from the server's own evaluation of that person's rules, so it
 * matches what happens when they connect.
 */
export function CheckAccess({
  resourceId,
  serverName,
}: {
  resourceId: string;
  serverName: string;
}): JSX.Element {
  const { data: membersData, isLoading } = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const [userId, setUserId] = useState<string>();

  const items = useMemo((): DropdownItem[] => {
    const members = membersData?.members ?? [];
    return [...members]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((member) => ({
        value: member.id,
        label: member.name,
        description: member.email,
        keywords: [member.email],
      }));
  }, [membersData?.members]);

  const selected = membersData?.members.find((member) => member.id === userId);

  return (
    <Card.Dashboard
      title="Check access"
      bodyClassName="p-0"
      headerClassName="py-2.5"
      action={
        selected && (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setUserId(undefined)}
          >
            <Button.LeftIcon>
              <X className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Check someone else</Button.Text>
          </Button>
        )
      }
    >
      <div className="px-6 py-4">
        <Combobox
          items={items}
          selected={userId}
          onSelectionChange={(item) => setUserId(item.value)}
          searchable
          searchPlaceholder="Search people"
          emptyMessage="No people match."
          variant="secondary"
          className="h-auto w-full justify-start py-2 text-left"
          contentClassName="w-[var(--radix-popover-trigger-width)]"
          disabledMessage={isLoading ? "Loading people" : undefined}
        >
          {selected ? (
            <span className="flex items-baseline gap-3">
              <span>{selected.name}</span>
              <Text as="span" muted small>
                {selected.email}
              </Text>
            </span>
          ) : (
            <span className="text-muted-foreground flex items-center gap-2 font-normal">
              <Search className="h-4 w-4" />
              Check a person: can they use {serverName}, and why?
            </span>
          )}
        </Combobox>
      </div>
      {selected && (
        <CheckAccessResult
          // A new person starts from their own Connect answer, not the level
          // the last person was left on.
          key={selected.id}
          resourceId={resourceId}
          userId={selected.id}
          memberName={selected.name}
          serverName={serverName}
        />
      )}
    </Card.Dashboard>
  );
}
