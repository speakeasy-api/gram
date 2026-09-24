import { useState } from "react";
import { z } from "zod";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Switch } from "@/components/ui/Switch";

const emailSchema = z.string().email();

type SetupTaskAssignmentDialogProps = {
  task: SetupTask | null;
  members: AccessMember[];
  roles: Role[];
  pending: boolean;
  onClose: () => void;
  onAssignMember: (userId: string) => void;
  onAssignEmail: (email: string, inviteRoleId?: string) => void;
  onUnassign: () => void;
};

export function SetupTaskAssignmentDialog({
  task,
  members,
  roles,
  pending,
  onClose,
  onAssignMember,
  onAssignEmail,
  onUnassign,
}: SetupTaskAssignmentDialogProps): JSX.Element {
  const [mode, setMode] = useState<"member" | "email">(
    task?.assignee?.userId ? "member" : task?.assignee ? "email" : "member",
  );
  const [memberId, setMemberId] = useState(task?.assignee?.userId ?? "");
  const [email, setEmail] = useState(
    task?.assignee && !task.assignee.userId ? task.assignee.email : "",
  );
  const [invite, setInvite] = useState(false);
  const memberRole = roles.find(
    (role) => role.slug === "member" || role.name.toLowerCase() === "member",
  );
  const [roleId, setRoleId] = useState("");
  const effectiveRoleId = roleId || memberRole?.id;
  const normalizedEmail = email.trim().toLowerCase();
  const validEmail = emailSchema.safeParse(normalizedEmail).success;

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (mode === "member") {
      if (memberId) onAssignMember(memberId);
      return;
    }
    if (validEmail)
      onAssignEmail(normalizedEmail, invite ? effectiveRoleId : undefined);
  };

  return (
    <Dialog
      open={task !== null}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <Dialog.Content
        closeable={!pending}
        onEscapeKeyDown={(event) => {
          if (pending) event.preventDefault();
        }}
        onInteractOutside={(event) => {
          if (pending) event.preventDefault();
        }}
      >
        <Dialog.Header>
          <Dialog.Title>Assign {task?.title}</Dialog.Title>
          <Dialog.Description>
            Choose an active member or assign an email address before they join.
          </Dialog.Description>
        </Dialog.Header>
        <form className="space-y-5" onSubmit={handleSubmit}>
          <div className="space-y-2">
            <label
              htmlFor="setup-assignment-type"
              className="text-sm font-medium"
            >
              Assignee type
            </label>
            <Select
              value={mode}
              onValueChange={(value) => setMode(value as "member" | "email")}
              disabled={pending}
            >
              <SelectTrigger id="setup-assignment-type" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">Active member</SelectItem>
                <SelectItem value="email">Email address</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {mode === "member" ? (
            <div className="space-y-2">
              <label
                htmlFor="setup-assignee-member"
                className="text-sm font-medium"
              >
                Member
              </label>
              <Select
                value={memberId}
                onValueChange={setMemberId}
                disabled={pending}
              >
                <SelectTrigger id="setup-assignee-member" className="w-full">
                  <SelectValue placeholder="Choose a member" />
                </SelectTrigger>
                <SelectContent>
                  {members.map((member) => (
                    <SelectItem key={member.id} value={member.id}>
                      {member.name} ({member.email})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : (
            <>
              <div className="space-y-2">
                <label
                  htmlFor="setup-assignee-email"
                  className="text-sm font-medium"
                >
                  Email address
                </label>
                <Input
                  id="setup-assignee-email"
                  type="email"
                  value={email}
                  onChange={setEmail}
                  placeholder="owner@example.com"
                  disabled={pending}
                  error={email.length > 0 && !validEmail}
                  required
                />
                {email.length > 0 && !validEmail ? (
                  <p className="text-xs text-default-destructive">
                    Enter a valid email address.
                  </p>
                ) : null}
              </div>
              <div className="flex items-center justify-between gap-4 border p-3">
                <div>
                  <p id="setup-invite-label" className="text-sm font-medium">
                    Send a team invite
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Assignment is saved before the invite is sent.
                  </p>
                </div>
                <Switch
                  checked={invite}
                  onCheckedChange={setInvite}
                  aria-labelledby="setup-invite-label"
                  disabled={pending}
                />
              </div>
              {invite ? (
                <div className="space-y-2">
                  <label
                    htmlFor="setup-invite-role"
                    className="text-sm font-medium"
                  >
                    Invite role
                  </label>
                  <Select
                    value={effectiveRoleId ?? ""}
                    onValueChange={setRoleId}
                    disabled={pending}
                  >
                    <SelectTrigger id="setup-invite-role" className="w-full">
                      <SelectValue placeholder="Choose a role" />
                    </SelectTrigger>
                    <SelectContent>
                      {roles.map((role) => (
                        <SelectItem key={role.id} value={role.id}>
                          {role.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
            </>
          )}

          <Dialog.Footer className="gap-2">
            {task?.assignee ? (
              <Button
                type="button"
                variant="tertiary"
                onClick={onUnassign}
                disabled={pending}
              >
                Unassign
              </Button>
            ) : null}
            <Button
              type="button"
              variant="secondary"
              onClick={onClose}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                pending ||
                (mode === "member"
                  ? !memberId
                  : !validEmail || (invite && !effectiveRoleId))
              }
            >
              {mode === "email" && invite ? "Assign and invite" : "Assign"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
