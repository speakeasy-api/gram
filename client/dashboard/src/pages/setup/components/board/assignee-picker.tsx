import { useMemo, useState } from "react";
import { Check, Mail, UserRoundPlus, X } from "lucide-react";
import { useListOrganizationUsers } from "@gram/client/react-query/listOrganizationUsers.js";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import { Button } from "@/components/ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { AssigneeAvatar } from "./assignee-avatar";
import { type Assignee, assigneeLabel } from "./board-store";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function looksLikeEmail(value: string): boolean {
  return EMAIL_PATTERN.test(value.trim());
}

function toAssignee(user: OrganizationUser): Assignee {
  return {
    kind: "user",
    userId: user.userId,
    name: user.name,
    email: user.email,
    photoUrl: user.photoUrl,
  };
}

function matchesQuery(user: OrganizationUser, query: string): boolean {
  return (
    user.name.toLowerCase().includes(query) ||
    user.email.toLowerCase().includes(query)
  );
}

interface AssigneePickerProps {
  assignee: Assignee | undefined;
  onChange: (assignee: Assignee | undefined) => void;
  /** Trigger label while nobody is assigned. */
  placeholder?: string;
  size?: "xs" | "sm";
}

/**
 * Hands a task to a member of the organization, or to an email address for
 * someone who has not joined yet. Team members come from the organization's
 * user list; typing a full address that matches nobody offers it as an
 * outside assignee.
 */
export function AssigneePicker({
  assignee,
  onChange,
  placeholder = "Assign",
  size = "xs",
}: AssigneePickerProps): JSX.Element {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { data, isLoading } = useListOrganizationUsers(undefined, undefined, {
    enabled: open,
  });

  const users = useMemo(
    () => [...(data?.users ?? [])].sort((a, b) => a.name.localeCompare(b.name)),
    [data],
  );
  const normalizedQuery = query.trim().toLowerCase();
  const matches = useMemo(
    () =>
      normalizedQuery
        ? users.filter((user) => matchesQuery(user, normalizedQuery))
        : users,
    [users, normalizedQuery],
  );
  const outsideEmail =
    looksLikeEmail(query) &&
    !users.some((user) => user.email.toLowerCase() === normalizedQuery)
      ? query.trim()
      : null;

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) setQuery("");
  };

  const select = (next: Assignee | undefined) => {
    onChange(next);
    handleOpenChange(false);
  };

  const currentIdentity =
    assignee?.kind === "user" ? assignee.userId : undefined;

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="tertiary"
          size={size}
          className="min-w-0 max-w-full gap-1.5 px-1.5 font-normal"
          aria-label={
            assignee ? `Assigned to ${assigneeLabel(assignee)}` : placeholder
          }
        >
          <Button.LeftIcon>
            {assignee ? (
              <AssigneeAvatar assignee={assignee} className="size-4" />
            ) : (
              <UserRoundPlus />
            )}
          </Button.LeftIcon>
          {/* Button.Text trims its box to cap height and baseline, and that
              trim reaches into nested blocks, so the overflow clip `truncate`
              needs would cut off ascenders and descenders. Padding the
              clipping span and pulling it back with negative margins keeps
              the layout height while giving the glyphs room. */}
          <Button.Text className="min-w-0">
            <span className="-my-1 block truncate py-1">
              {assignee ? assigneeLabel(assignee) : placeholder}
            </span>
          </Button.Text>
        </Button>
      </PopoverTrigger>
      {/* The popover is portaled, so React still bubbles its events up to the
          card that owns this picker; stop them here so choosing a person never
          doubles as clicking the card. */}
      <PopoverContent
        align="start"
        className="w-72 p-0"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false} label="Assign task">
          <CommandInput
            placeholder="Search team or enter an email"
            value={query}
            onValueChange={setQuery}
            className="h-9"
          />
          <CommandList>
            {assignee && (
              <CommandGroup>
                <CommandItem
                  value="__unassign"
                  onSelect={() => select(undefined)}
                  className="cursor-pointer"
                >
                  <X />
                  Unassign
                </CommandItem>
              </CommandGroup>
            )}
            <CommandGroup heading="Team">
              {isLoading && (
                <CommandItem value="__loading" disabled>
                  Loading team…
                </CommandItem>
              )}
              {matches.map((user) => (
                <CommandItem
                  key={user.userId}
                  value={user.userId}
                  onSelect={() => select(toAssignee(user))}
                  className="cursor-pointer"
                >
                  <AssigneeAvatar assignee={toAssignee(user)} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm">{user.name}</div>
                    <div className="text-muted-foreground truncate text-xs">
                      {user.email}
                    </div>
                  </div>
                  {currentIdentity === user.userId && (
                    <Check className="size-4 shrink-0" />
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
            {outsideEmail && (
              <CommandGroup heading="Outside the team">
                <CommandItem
                  value={outsideEmail}
                  onSelect={() =>
                    select({ kind: "email", email: outsideEmail })
                  }
                  className="cursor-pointer"
                >
                  <Mail />
                  <span className="truncate">Assign {outsideEmail}</span>
                </CommandItem>
              </CommandGroup>
            )}
            <CommandEmpty>
              No team member matches. Enter a full email address to assign
              someone who has not joined yet.
            </CommandEmpty>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
