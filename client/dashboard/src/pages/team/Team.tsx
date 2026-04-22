import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useOrganization, useUser } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { formatDistanceToNow } from "date-fns";
import {
  invalidateAllListInvites,
  invalidateAllListOrganizationUsers,
  useListOrganizationUsersSuspense,
  useListInvitesSuspense,
  useSendInviteMutation,
  useRevokeInviteMutation,
  useRemoveOrganizationUserMutation,
} from "@gram/client/react-query";
import {
  OrganizationUser,
  OrganizationInvitation,
} from "@gram/client/models/components";
import { Button, Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2, UserPlus, Users, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RequireScope } from "@/components/require-scope";

function getMemberColors(id: string) {
  let hash = 2166136261;
  for (let i = 0; i < id.length; i++) {
    hash ^= id.charCodeAt(i);
    hash +=
      (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
  }
  hash = hash >>> 0;
  const hue1 = hash % 360;
  const hue2 = (hue1 + ((hash >> 8) % 360)) % 360;
  const saturation = Math.max(65, (hash >> 16) % 100);
  const angle = (hash >> 24) % 360;
  return {
    from: `hsl(${hue1}, ${saturation}%, 65%)`,
    to: `hsl(${hue2}, ${saturation}%, 60%)`,
    angle,
  };
}

export default function Team() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <TeamInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

export function TeamInner() {
  const organization = useOrganization();
  const user = useUser();
  const queryClient = useQueryClient();

  const [isInviteDialogOpen, setIsInviteDialogOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [memberToRemove, setMemberToRemove] = useState<OrganizationUser | null>(
    null,
  );
  const [inviteToCancel, setInviteToCancel] =
    useState<OrganizationInvitation | null>(null);

  const { data: membersData } = useListOrganizationUsersSuspense();
  const { data: invitesData } = useListInvitesSuspense();

  const members = membersData?.users ?? [];
  const invites = invitesData?.invitations ?? [];

  const invalidateTeamData = async () => {
    await Promise.all([
      invalidateAllListOrganizationUsers(queryClient),
      invalidateAllListInvites(queryClient),
    ]);
  };

  const inviteMutation = useSendInviteMutation({
    onError: () => {
      toast.error("Failed to send invite");
    },
  });

  const removeMemberMutation = useRemoveOrganizationUserMutation({
    onError: () => {
      toast.error("Failed to remove member");
    },
  });

  const revokeInviteMutation = useRevokeInviteMutation({
    onError: () => {
      toast.error("Failed to cancel invite");
    },
  });

  const handleInvite = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const submittedEmail = inviteEmail.trim();
    if (!submittedEmail) return;

    inviteMutation.mutate(
      {
        request: {
          email: submittedEmail,
        },
      },
      {
        onSuccess: async () => {
          await invalidateTeamData();
          toast.success(`Invite sent to ${submittedEmail}`);
          setInviteEmail("");
          setIsInviteDialogOpen(false);
        },
      },
    );
  };

  const handleRemoveMember = () => {
    if (!memberToRemove || memberToRemove.email === user.email) return;

    const displayName = memberToRemove.name ?? memberToRemove.email;
    removeMemberMutation.mutate(
      {
        request: {
          userId: memberToRemove.userId,
        },
      },
      {
        onSuccess: async () => {
          await invalidateTeamData();
          toast.success(`${displayName} has been removed`);
          setMemberToRemove(null);
        },
      },
    );
  };

  const handleRevokeInvite = () => {
    if (!inviteToCancel) return;

    const email = inviteToCancel.email;
    revokeInviteMutation.mutate(
      {
        request: {
          invitationId: inviteToCancel.id,
        },
      },
      {
        onSuccess: async () => {
          await invalidateTeamData();
          toast.success(`Invite to ${email} has been cancelled`);
          setInviteToCancel(null);
        },
      },
    );
  };

  const memberColumns: Column<OrganizationUser>[] = [
    {
      key: "member",
      header: "Member",
      width: "1fr",
      render: (member) => (
        <Stack direction="horizontal" align="center" gap={3}>
          {member.photoUrl ? (
            <img
              src={member.photoUrl}
              alt={member.name}
              className="h-8 w-8 rounded-full"
            />
          ) : (
            <div
              className="flex h-8 w-8 items-center justify-center rounded-full text-xs font-medium text-white"
              style={{
                backgroundImage: `linear-gradient(${getMemberColors(member.id).angle}deg, ${getMemberColors(member.id).from}, ${getMemberColors(member.id).to})`,
              }}
            >
              {member.name
                .split(" ")
                .map((n) => n[0])
                .join("")
                .toUpperCase()
                .slice(0, 2)}
            </div>
          )}
          <Stack direction="vertical" gap={0}>
            <Type variant="body" className="font-medium">
              {member.name}
            </Type>
            <Type variant="body" className="text-muted-foreground text-sm">
              {member.email}
            </Type>
          </Stack>
        </Stack>
      ),
    },
    {
      key: "joinedAt",
      header: "Joined",
      width: "200px",
      render: (member) => (
        <Type
          variant="body"
          className="text-muted-foreground whitespace-nowrap"
        >
          <HumanizeDateTime date={member.createdAt} />
        </Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (member) =>
        member.email !== user.email ? (
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setMemberToRemove(member)}
              className="hover:text-destructive"
            >
              <Button.LeftIcon>
                <Trash2 className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text className="sr-only">Remove member</Button.Text>
            </Button>
          </RequireScope>
        ) : (
          <Type variant="body" className="text-muted-foreground text-sm">
            You
          </Type>
        ),
    },
  ];

  const inviteColumns: Column<OrganizationInvitation>[] = [
    {
      key: "email",
      header: "Email",
      width: "1fr",
      render: (invite) => {
        const isExpired = invite.state === "expired";
        return (
          <Stack
            direction="horizontal"
            align="center"
            gap={3}
            className={isExpired ? "opacity-50" : ""}
          >
            <div
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-medium text-white"
              style={{
                backgroundImage: `linear-gradient(${getMemberColors(invite.email).angle}deg, ${getMemberColors(invite.email).from}, ${getMemberColors(invite.email).to})`,
              }}
            >
              {invite.email
                .split("@")[0]
                ?.replace(/[^a-zA-Z]/g, "")
                .slice(0, 2)
                .toUpperCase() || "?"}
            </div>
            <Type variant="body">{invite.email}</Type>
          </Stack>
        );
      },
    },
    {
      key: "createdAt",
      header: "Sent",
      width: "200px",
      render: (invite) => (
        <Type
          variant="body"
          className={`text-muted-foreground whitespace-nowrap ${invite.state === "expired" ? "opacity-50" : ""}`}
        >
          <HumanizeDateTime date={invite.createdAt} />
        </Type>
      ),
    },
    {
      key: "expiresAt",
      header: "Expires",
      width: "150px",
      render: (invite) => {
        const isExpired =
          invite.state === "expired" ||
          (invite.expiresAt && invite.expiresAt < new Date());
        return (
          <Type
            variant="body"
            className={isExpired ? "text-destructive" : "text-muted-foreground"}
          >
            {isExpired
              ? "Expired"
              : invite.expiresAt
                ? formatDistanceToNow(invite.expiresAt, { addSuffix: true })
                : "—"}
          </Type>
        );
      },
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (invite) => (
        <RequireScope scope="org:admin" level="component">
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setInviteToCancel(invite)}
            className="hover:text-destructive"
          >
            <Button.LeftIcon>
              <X className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text className="sr-only">Revoke invite</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  return (
    <>
      <Stack direction="vertical" gap={8}>
        {/* Members Section */}
        <div>
          <Stack
            direction="horizontal"
            justify="space-between"
            align="center"
            className="mb-4"
          >
            <Stack direction="vertical" gap={1}>
              <Heading variant="h4">Team Members</Heading>
              <Type variant="body" className="text-muted-foreground">
                Manage who has access to {organization.name}
              </Type>
            </Stack>
            <RequireScope scope="org:admin" level="component">
              <Button onClick={() => setIsInviteDialogOpen(true)}>
                <Button.LeftIcon>
                  <UserPlus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Invite Member</Button.Text>
              </Button>
            </RequireScope>
          </Stack>

          <Table
            columns={memberColumns}
            data={members}
            rowKey={(row) => row.userId}
            className="min-h-fit"
            noResultsMessage={
              <Stack
                gap={2}
                className="bg-background h-full p-8"
                align="center"
                justify="center"
              >
                <Users className="text-muted-foreground h-12 w-12" />
                <Type variant="body" className="text-muted-foreground">
                  No team members yet
                </Type>
              </Stack>
            }
          />
        </div>

        {/* Pending Invites Section */}
        {invites.length > 0 && (
          <div>
            <Stack direction="vertical" gap={1} className="mb-4">
              <Heading variant="h4">Pending Invites</Heading>
              <Type variant="body" className="text-muted-foreground">
                Invitations that haven't been accepted yet
              </Type>
            </Stack>

            <Table
              columns={inviteColumns}
              data={invites}
              rowKey={(row) => row.id}
              className="min-h-fit"
            />
          </div>
        )}
      </Stack>

      {/* Invite Dialog */}
      <Dialog open={isInviteDialogOpen} onOpenChange={setIsInviteDialogOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Invite Team Member</Dialog.Title>
          </Dialog.Header>
          <form className="space-y-4 py-4" onSubmit={handleInvite}>
            <Type variant="body" className="text-muted-foreground">
              Enter the email address of the person you'd like to invite to{" "}
              <span className="text-foreground font-medium">
                {organization.name}
              </span>
              .
            </Type>
            <InputField
              label="Email address"
              name="email"
              type="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              placeholder="colleague@company.com"
              required
              autoFocus
              autoCapitalize="off"
              autoComplete="off"
              autoCorrect="off"
            />
            <div className="flex justify-end space-x-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => setIsInviteDialogOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={inviteMutation.isPending || !inviteEmail.trim()}
              >
                {inviteMutation.isPending ? "Sending..." : "Send Invite"}
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog>

      {/* Remove Member Dialog */}
      <Dialog
        open={!!memberToRemove}
        onOpenChange={(open) => !open && setMemberToRemove(null)}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Remove Team Member</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to remove{" "}
              <span className="font-bold">{memberToRemove?.name}</span> from{" "}
              {organization.name}? They will lose access to all projects and
              resources.
            </Type>
            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={() => setMemberToRemove(null)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleRemoveMember}
                disabled={removeMemberMutation.isPending}
              >
                {removeMemberMutation.isPending
                  ? "Removing..."
                  : "Remove Member"}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      {/* Cancel Invite Dialog */}
      <Dialog
        open={!!inviteToCancel}
        onOpenChange={(open) => !open && setInviteToCancel(null)}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Cancel Invite</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to cancel the invite to{" "}
              <span className="font-bold">{inviteToCancel?.email}</span>?
            </Type>
            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={() => setInviteToCancel(null)}
              >
                Keep Invite
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleRevokeInvite}
                disabled={revokeInviteMutation.isPending}
              >
                {revokeInviteMutation.isPending
                  ? "Revoking..."
                  : "Revoke Invite"}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
