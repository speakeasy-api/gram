import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useOrganization, useUser } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Mail, Send, Trash2, UserPlus, Users, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

// Placeholder types until generated SDK is available
interface TeamMember {
  id: string;
  email: string;
  displayName: string;
  photoUrl?: string;
  joinedAt: string;
}

interface TeamInvite {
  id: string;
  email: string;
  status: string;
  invitedBy: string;
  createdAt: string;
  expiresAt: string;
}

export default function Team() {
  const organization = useOrganization();
  const user = useUser();
  const queryClient = useQueryClient();

  const [isInviteDialogOpen, setIsInviteDialogOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [memberToRemove, setMemberToRemove] = useState<TeamMember | null>(null);
  const [inviteToCancel, setInviteToCancel] = useState<TeamInvite | null>(null);
  const [isInviting, setIsInviting] = useState(false);

  // TODO: Replace with actual API hooks when generated
  // const { data: membersData } = useListTeamMembersSuspense({ organizationId: organization.id });
  // const { data: invitesData } = useListTeamInvitesSuspense({ organizationId: organization.id });

  // Placeholder data - will be replaced with actual API data
  const members: TeamMember[] = [
    {
      id: user.id,
      email: user.email,
      displayName: user.displayName || user.email,
      photoUrl: user.photoUrl,
      joinedAt: new Date().toISOString(),
    },
  ];
  const invites: TeamInvite[] = [];

  const handleInvite = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;

    setIsInviting(true);
    try {
      // TODO: Call inviteTeamMember mutation
      // await inviteMutation.mutateAsync({
      //   request: { organizationId: organization.id, email: inviteEmail }
      // });
      toast.success(`Invite sent to ${inviteEmail}`);
      setInviteEmail("");
      setIsInviteDialogOpen(false);
    } catch {
      toast.error("Failed to send invite");
    } finally {
      setIsInviting(false);
    }
  };

  const handleRemoveMember = async () => {
    if (!memberToRemove) return;

    try {
      // TODO: Call removeTeamMember mutation
      toast.success(`${memberToRemove.displayName} has been removed`);
      setMemberToRemove(null);
    } catch {
      toast.error("Failed to remove member");
    }
  };

  const handleCancelInvite = async () => {
    if (!inviteToCancel) return;

    try {
      // TODO: Call cancelTeamInvite mutation
      toast.success(`Invite to ${inviteToCancel.email} has been cancelled`);
      setInviteToCancel(null);
    } catch {
      toast.error("Failed to cancel invite");
    }
  };

  const handleResendInvite = async (invite: TeamInvite) => {
    try {
      // TODO: Call resendTeamInvite mutation
      toast.success(`Invite resent to ${invite.email}`);
    } catch {
      toast.error("Failed to resend invite");
    }
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
      render: (member) => <HumanizeDateTime date={member.joinedAt} />,
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
      width: "150px",
      render: (invite) => <HumanizeDateTime date={invite.createdAt} />,
    },
    {
      key: "actions",
      header: "",
      width: "120px",
      render: (invite) => (
        <Stack direction="horizontal" gap={1}>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => handleResendInvite(invite)}
            title="Resend invite"
          >
            <Button.LeftIcon>
              <Send className="h-4 w-4" />
            </Button.LeftIcon>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setInviteToCancel(invite)}
            className="hover:text-destructive"
            title="Cancel invite"
          >
            <Button.LeftIcon>
              <X className="h-4 w-4" />
            </Button.LeftIcon>
          </Button>
        </Stack>
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
                onChange={setInviteEmail}
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
                <Button type="submit" disabled={isInviting || !inviteEmail.trim()}>
                  {isInviting ? "Sending..." : "Send Invite"}
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
                <Button variant="destructive-primary" onClick={handleRemoveMember}>
                  Remove Member
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
                <Button variant="destructive-primary" onClick={handleCancelInvite}>
                  Cancel Invite
                </Button>
              </div>
            </div>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
