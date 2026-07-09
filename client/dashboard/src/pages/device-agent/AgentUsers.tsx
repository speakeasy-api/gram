import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useMembers } from "@gram/client/react-query/members.js";
import { useSyncedAgentUsers } from "@gram/client/react-query/syncedAgentUsers.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { SyncedAgentUser } from "@gram/client/models/components/syncedagentuser.js";
import { Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { Laptop } from "lucide-react";
import { useMemo } from "react";

// A device counts as "active" if it has synced within this window. The agent
// polls every ~60s, so five minutes tolerates a few missed polls (sleep, brief
// network loss) before we flag the device as stale.
const ACTIVE_WINDOW_MS = 5 * 60 * 1000;

// initialsFor derives up to two uppercase initials from a display name, falling
// back to the first character of the email's local part.
function initialsFor(nameOrEmail: string): string {
  const trimmed = nameOrEmail.trim();
  const parts = trimmed.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
  }
  const local = trimmed.split("@")[0] ?? trimmed;
  return local.slice(0, 2).toUpperCase();
}

// hueFor spreads emails across the color wheel deterministically so each user's
// fallback avatar keeps a stable, distinct color.
function hueFor(email: string): number {
  let hash = 0;
  for (let i = 0; i < email.length; i++) {
    hash = (hash * 31 + email.charCodeAt(i)) % 360;
  }
  return hash;
}

function UserCell({
  user,
  member,
}: {
  user: SyncedAgentUser;
  member: AccessMember | undefined;
}) {
  const displayName = member?.name || user.email;
  const hue = hueFor(user.email);
  return (
    <Stack direction="horizontal" align="center" gap={3}>
      {member?.photoUrl ? (
        <img
          src={member.photoUrl}
          alt={displayName}
          className="h-8 w-8 rounded-full"
        />
      ) : (
        <div
          className="flex h-8 w-8 items-center justify-center rounded-full text-xs font-medium text-white"
          style={{
            backgroundImage: `linear-gradient(135deg, hsl(${hue} 65% 55%), hsl(${(hue + 40) % 360} 65% 45%))`,
          }}
        >
          {initialsFor(displayName)}
        </div>
      )}
      <Stack direction="vertical" gap={0}>
        <Type variant="body" className="font-medium">
          {displayName}
        </Type>
        {/* Only show the email as a subtitle when it isn't already the title. */}
        {member?.name ? (
          <Type variant="body" className="text-muted-foreground text-sm">
            {user.email}
          </Type>
        ) : null}
      </Stack>
    </Stack>
  );
}

// AgentUsers lists the org members whose device agent has polled the control
// plane, attributed by the email the agent reports on each sync. Rows are
// returned most-recently-active first by the server. Org admins only — the tab
// hosting it is gated on org:admin, and the endpoint enforces the same.
export function AgentUsers(): React.JSX.Element {
  const { data, isLoading, isError } = useSyncedAgentUsers();
  const { data: membersData } = useMembers();

  const membersByEmail = useMemo(() => {
    const map = new Map<string, AccessMember>();
    for (const m of membersData?.members ?? []) {
      map.set(m.email.toLowerCase(), m);
    }
    return map;
  }, [membersData]);

  const users = data?.users ?? [];

  const columns: Column<SyncedAgentUser>[] = [
    {
      key: "user",
      header: "User",
      width: "1fr",
      render: (user) => (
        <UserCell
          user={user}
          member={membersByEmail.get(user.email.toLowerCase())}
        />
      ),
    },
    {
      key: "lastSync",
      header: "Last sync",
      width: "220px",
      render: (user) => (
        <Type
          variant="body"
          className="text-muted-foreground whitespace-nowrap"
        >
          <HumanizeDateTime date={user.lastSeenAt} />
        </Type>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "120px",
      render: (user) => {
        const active =
          Date.now() - user.lastSeenAt.getTime() < ACTIVE_WINDOW_MS;
        return active ? (
          <Badge variant="secondary">Active</Badge>
        ) : (
          <Badge variant="outline">Stale</Badge>
        );
      },
    },
  ];

  if (isLoading) {
    return (
      <Type small muted>
        Loading device agent activity…
      </Type>
    );
  }

  if (isError) {
    return (
      <Alert variant="warning">
        <Icon name="triangle-alert" className="h-4 w-4" />
        <AlertTitle>Couldn't load device agent activity</AlertTitle>
        <AlertDescription>
          Something went wrong fetching who's running the agent. Try refreshing
          the page.
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Type muted>
        Users whose device agent has checked in with Speakeasy, attributed by
        the email each agent reports. The agent polls about every minute; a
        device is <strong className="text-foreground">Active</strong> when it
        has synced in the last few minutes, otherwise <strong>Stale</strong>.
      </Type>
      <Table
        columns={columns}
        data={users}
        rowKey={(row) => row.email}
        className="min-h-fit"
        noResultsMessage={
          <Stack
            gap={2}
            className="bg-background p-8"
            align="center"
            justify="center"
          >
            <Laptop className="text-muted-foreground h-10 w-10" />
            <Type variant="body" className="font-medium">
              No device agents have synced yet
            </Type>
            <Type muted small className="text-center">
              Once a user installs the agent and it enrolls, they'll appear here
              within a minute.
            </Type>
          </Stack>
        }
      />
    </div>
  );
}
