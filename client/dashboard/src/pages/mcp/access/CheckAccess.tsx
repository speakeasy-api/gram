import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
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
  const {
    data: membersData,
    isLoading,
    isError,
  } = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const [userId, setUserId] = useState<string>();
  // A failed background refetch keeps the members already loaded, so only a
  // lookup that never returned anyone counts as failed.
  const failed = isError && !membersData;

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
      // A gray title bar over white rows, so the card does not read as one more
      // white panel in a stack.
      className="bg-background h-auto"
      bodyClassName="bg-card p-0"
      // Fixed heights: the bar and the picker stay the same size whether or
      // not someone is picked, so choosing a person does not shift the page.
      headerClassName="h-12 py-0"
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
          className="h-10 w-full justify-start text-left"
          contentClassName="w-[var(--radix-popover-trigger-width)]"
          disabledMessage={membersUnavailable(isLoading, failed)}
        >
          <PickerLabel
            failed={failed}
            selected={selected}
            serverName={serverName}
          />
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

/** Why the picker cannot be used yet, when it cannot. */
function membersUnavailable(
  isLoading: boolean,
  failed: boolean,
): string | undefined {
  if (failed) return "People could not be loaded";
  if (isLoading) return "Loading people";
  return undefined;
}

/** What the picker shows when closed: a failure, the chosen person, or a prompt. */
function PickerLabel({
  failed,
  selected,
  serverName,
}: {
  failed: boolean;
  selected: AccessMember | undefined;
  serverName: string;
}): JSX.Element {
  if (failed) {
    return (
      <span className="text-muted-foreground truncate font-sans font-normal">
        People could not be loaded. Reload the page to try again.
      </span>
    );
  }
  if (selected) {
    return (
      <span className="flex min-w-0 items-baseline gap-3 font-sans">
        <span className="min-w-0 truncate font-medium">{selected.name}</span>
        <Text as="span" muted small className="truncate">
          {selected.email}
        </Text>
      </span>
    );
  }
  return (
    <span className="text-muted-foreground flex items-center gap-2 font-sans font-normal">
      <Search className="h-4 w-4" />
      Check a person: can they use {serverName}, and why?
    </span>
  );
}
