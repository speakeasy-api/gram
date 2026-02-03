import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useOrganization, useUser } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { formatDistanceToNow } from "date-fns";
import {
  invalidateAllListTeamInvites,
  invalidateAllListTeamMembers,
  useCancelTeamInviteMutation,
  useInviteTeamMemberMutation,
  useListTeamInvitesSuspense,
  useListTeamMembersSuspense,
  useRemoveTeamMemberMutation,
  useResendTeamInviteMutation,
} from "@gram/client/react-query";
import { TeamInvite, TeamMember } from "@gram/client/models/components";
import { Button, Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Mail, Send, Trash2, UserPlus, Users, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

export default function Team() {
  const organization = useOrganization();
  const user = useUser();
  const queryClient = useQueryClient();

  const [isInviteDialogOpen, setIsInviteDialogOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [memberToRemove, setMemberToRemove] = useState<TeamMember | null>(null);
  const [inviteToCancel, setInviteToCancel] = useState<TeamInvite | null>(null);

  const { data: membersData } = useListTeamMembersSuspense({
    organizationId: organization.id,
  });
  const { data: invitesData } = useListTeamInvitesSuspense({
    organizationId: organization.id,
  });

  const members = membersData?.members ?? [];
  const invites = invitesData?.invites ?? [];

  const invalidateTeamData = async () => {
    await Promise.all([
      invalidateAllListTeamMembers(queryClient),
      invalidateAllListTeamInvites(queryClient),
    ]);
  };

  const inviteMutation = useInviteTeamMemberMutation({
    onSuccess: async () => {
      await invalidateTeamData();
      toast.success(`Invite sent to ${inviteEmail}`);
      setInviteEmail("");
      setIsInviteDialogOpen(false);
    },
    onError: () => {
      toast.error("Failed to send invite");
    },
  });

  const removeMemberMutation = useRemoveTeamMemberMutation({
    onSuccess: async () => {
      await invalidateTeamData();
      toast.success(`${memberToRemove?.displayName} has been removed`);
      setMemberToRemove(null);
    },
    onError: () => {
      toast.error("Failed to remove member");
    },
  });

  const cancelInviteMutation = useCancelTeamInviteMutation({
    onSuccess: async () => {
      await invalidateTeamData();
      toast.success(`Invite to ${inviteToCancel?.email} has been cancelled`);
      setInviteToCancel(null);
    },
    onError: () => {
      toast.error("Failed to cancel invite");
    },
  });

  const resendInviteMutation = useResendTeamInviteMutation({
    onSuccess: async () => {
      await invalidateTeamData();
      toast.success("Invite resent");
    },
    onError: () => {
      toast.error("Failed to resend invite");
    },
  });

  const handleInvite = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;

    inviteMutation.mutate({
      request: {
        inviteMemberForm: {
          organizationId: organization.id,
          email: inviteEmail,
        },
      },
    });
  };

  const handleRemoveMember = () => {
    if (!memberToRemove) return;

    removeMemberMutation.mutate({
      request: {
        organizationId: organization.id,
        userId: memberToRemove.id,
      },
    });
  };

  const handleCancelInvite = () => {
    if (!inviteToCancel) return;

    cancelInviteMutation.mutate({
      request: {
        inviteId: inviteToCancel.id,
      },
    });
  };

  const handleResendInvite = (invite: TeamInvite) => {
    resendInviteMutation.mutate({
      request: {
        resendInviteRequestBody: {
          inviteId: invite.id,
        },
      },
    });
  };

  const memberColumns: Column<TeamMember>[] = [
    {
      key: "member",
      header: "Member",
      width: "1fr",
      render: (member) => (
        <Stack direction="horizontal" align="center" gap={3}>
          {member.photoUrl ? (
            <img
              src={member.photoUrl}
              alt={member.displayName}
              className="w-8 h-8 rounded-full"
            />
          ) : (
            <div className="w-8 h-8 rounded-full bg-muted flex items-center justify-center">
              <Users className="w-4 h-4 text-muted-foreground" />
            </div>
          )}
          <Stack direction="vertical" gap={0}>
            <Type variant="body" className="font-medium">
              {member.displayName}
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
          <HumanizeDateTime date={member.joinedAt} />
        </Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (member) =>
        member.id !== user.id ? (
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
        ) : (
          <Type variant="body" className="text-muted-foreground text-sm">
            You
          </Type>
        ),
    },
  ];

  const inviteColumns: Column<TeamInvite>[] = [
    {
      key: "email",
      header: "Email",
      width: "1fr",
      render: (invite) => (
        <Stack direction="horizontal" align="center" gap={3}>
          <div className="w-8 h-8 rounded-full bg-muted flex items-center justify-center">
            <Mail className="w-4 h-4 text-muted-foreground" />
          </div>
          <Type variant="body">{invite.email}</Type>
        </Stack>
      ),
    },
    {
      key: "invitedBy",
      header: "Invited by",
      width: "200px",
      render: (invite) => <Type variant="body">{invite.invitedBy}</Type>,
    },
    {
      key: "createdAt",
      header: "Sent",
      width: "200px",
      render: (invite) => (
        <Type
          variant="body"
          className="text-muted-foreground whitespace-nowrap"
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
        const now = new Date();
        const isExpired = invite.expiresAt < now;
        return (
          <Type
            variant="body"
            className={isExpired ? "text-destructive" : "text-muted-foreground"}
          >
            {isExpired
              ? "Expired"
              : formatDistanceToNow(invite.expiresAt, { addSuffix: true })}
          </Type>
        );
      },
    },
    {
      key: "actions",
      header: "",
      width: "120px",
      render: (invite) => (
        <TooltipProvider delayDuration={0}>
          <Stack direction="horizontal" gap={1}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => handleResendInvite(invite)}
                >
                  <Button.LeftIcon>
                    <Send className="h-4 w-4" />
                  </Button.LeftIcon>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Resend invite</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => setInviteToCancel(invite)}
                  className="hover:text-destructive"
                >
                  <Button.LeftIcon>
                    <X className="h-4 w-4" />
                  </Button.LeftIcon>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Cancel invite</TooltipContent>
            </Tooltip>
          </Stack>
        </TooltipProvider>
      ),
    },
  ];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
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
              <Button onClick={() => setIsInviteDialogOpen(true)}>
                <Button.LeftIcon>
                  <UserPlus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Invite Member</Button.Text>
              </Button>
            </Stack>

            <Table
              columns={memberColumns}
              data={members}
              rowKey={(row) => row.id}
              className="min-h-fit"
              noResultsMessage={
                <Stack
                  gap={2}
                  className="h-full p-8 bg-background"
                  align="center"
                  justify="center"
                >
                  <Users className="h-12 w-12 text-muted-foreground" />
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
                <span className="font-medium text-foreground">
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
                <span className="font-bold">{memberToRemove?.displayName}</span>{" "}
                from {organization.name}? They will lose access to all projects
                and resources.
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
                  onClick={handleCancelInvite}
                  disabled={cancelInviteMutation.isPending}
                >
                  {cancelInviteMutation.isPending
                    ? "Cancelling..."
                    : "Cancel Invite"}
                </Button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
