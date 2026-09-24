import { useMemo, useRef, useState } from "react";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useSendInviteMutation } from "@gram/client/react-query/sendInvite.js";
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
  onChange: (
    assignee: Assignee | undefined,
  ) => Promise<boolean> | boolean | void;
  ownerBreakdown?: string;
  bulkAssignment?: boolean;
  /** Trigger label while nobody is assigned. */
  placeholder?: string;
  size?: "xs" | "sm";
  disabled?: boolean;
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
  disabled: externallyDisabled = false,
  ownerBreakdown,
  bulkAssignment = false,
}: AssigneePickerProps): JSX.Element {
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const disabled = externallyDisabled || busy;
  const [invite, setInvite] = useState(false);
  const [roleId, setRoleId] = useState("");
  const [failedInvite, setFailedInvite] = useState<{
    email: string;
    roleId: string;
  } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const sendInvite = useSendInviteMutation();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { data, isLoading, isError, isSuccess, refetch } =
    useListOrganizationUsers(undefined, undefined, {
      enabled: open && !disabled,
      throwOnError: false,
    });

  const rolesQuery = useRoles(undefined, undefined, {
    enabled: open && !externallyDisabled,
    throwOnError: false,
  });
  const roles = rolesQuery.data?.roles ?? [];
  const effectiveRoleId =
    roleId ||
    roles.find(
      (role) => role.slug === "member" || role.name.toLowerCase() === "member",
    )?.id;
  const sendTeamInvite = async (pendingInvite: {
    email: string;
    roleId: string;
  }) => {
    try {
      await sendInvite.mutateAsync({
        request: { sendInviteRequestBody: pendingInvite },
      });
      setFailedInvite(null);
      setActionError(null);
      handleOpenChange(false);
    } catch {
      setFailedInvite(pendingInvite);
      setActionError(
        "Assignment saved, but the team invite failed. Retry without reassigning tasks.",
      );
    }
  };
  const retryInvite = async () => {
    if (disabled || busyRef.current || !failedInvite) return;
    busyRef.current = true;
    setBusy(true);
    try {
      await sendTeamInvite(failedInvite);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };
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
    isSuccess &&
    looksLikeEmail(query) &&
    !users.some((user) => user.email.toLowerCase() === normalizedQuery)
      ? query.trim()
      : null;

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setQuery("");
      setInvite(false);
    }
  };

  const select = async (next: Assignee | undefined) => {
    if (
      disabled ||
      busyRef.current ||
      (next?.kind === "email" && invite && !effectiveRoleId)
    )
      return;
    busyRef.current = true;
    setBusy(true);
    setActionError(null);
    try {
      if ((await onChange(next)) === false) return;
      setFailedInvite(null);
      if (next?.kind === "email" && invite && effectiveRoleId) {
        await sendTeamInvite({ email: next.email, roleId: effectiveRoleId });
      } else {
        handleOpenChange(false);
      }
    } catch {
      setActionError("Could not save assignment. Try again.");
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const currentIdentity =
    assignee?.kind === "user" ? assignee.userId : undefined;

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="tertiary"
          size={size}
          edge="start"
          disabled={disabled}
          className="min-w-0 max-w-full font-normal"
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
              {failedInvite ? " · Invite failed" : ""}
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
        {(ownerBreakdown || bulkAssignment) && (
          <div className="space-y-1 border-b p-3 text-xs text-muted-foreground">
            {ownerBreakdown && <p>{ownerBreakdown}</p>}
            {bulkAssignment && (
              <p>
                Reassignment applies to every task in this workstream, including
                hidden and optional tasks.
              </p>
            )}
          </div>
        )}
        {actionError && (
          <p role="alert" className="p-3 text-xs text-destructive">
            {actionError}
          </p>
        )}
        {failedInvite && (
          <div className="p-3 text-xs">
            <p>Team invite to {failedInvite.email} not sent.</p>
            <Button
              size="xs"
              disabled={disabled}
              onClick={() => void retryInvite()}
            >
              Retry team invite
            </Button>
          </div>
        )}
        {outsideEmail && (
          <div className="space-y-2 border-b p-3 text-xs">
            <p>
              Assignment sends a task notification email. It does not grant
              organization membership.
            </p>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={invite}
                disabled={disabled}
                onChange={(event) => setInvite(event.target.checked)}
              />
              Send a team invite
            </label>
            {invite && (
              <>
                <label className="block">
                  Invite role
                  <select
                    aria-label="Invite role"
                    className="mt-1 w-full rounded border bg-background p-2"
                    value={effectiveRoleId ?? ""}
                    disabled={disabled}
                    onChange={(event) => setRoleId(event.target.value)}
                  >
                    <option value="">Choose a role</option>
                    {roles.map((role) => (
                      <option key={role.id} value={role.id}>
                        {role.name}
                      </option>
                    ))}
                  </select>
                </label>
                {rolesQuery.isError && (
                  <Button size="xs" onClick={() => void rolesQuery.refetch()}>
                    Retry roles
                  </Button>
                )}
                <p>Assignment is saved before the membership invite is sent.</p>
              </>
            )}
          </div>
        )}
        <Command shouldFilter={false} label="Assign workstream">
          <CommandInput
            placeholder="Search team or enter an email"
            value={query}
            onValueChange={setQuery}
            className="h-9"
          />
          <CommandList>
            {isError && (
              <div role="alert" className="p-3 text-sm">
                Could not load team members.
                <Button size="sm" onClick={() => void refetch()}>
                  Retry
                </Button>
              </div>
            )}
            {(assignee || ownerBreakdown) && (
              <CommandGroup>
                <CommandItem
                  disabled={disabled}
                  value="__unassign"
                  onSelect={() => void select(undefined)}
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
              {isSuccess &&
                matches.map((user) => (
                  <CommandItem
                    disabled={disabled}
                    key={user.userId}
                    value={user.userId}
                    onSelect={() => void select(toAssignee(user))}
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
                  disabled={disabled || (invite && !effectiveRoleId)}
                  onSelect={() =>
                    void select({ kind: "email", email: outsideEmail })
                  }
                  className="cursor-pointer"
                >
                  <Mail />
                  <span className="truncate">Assign {outsideEmail}</span>
                </CommandItem>
              </CommandGroup>
            )}
            {isSuccess && (
              <CommandEmpty>
                No team member matches. Enter a full email address to assign
                someone who has not joined yet.
              </CommandEmpty>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
