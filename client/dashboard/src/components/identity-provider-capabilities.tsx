import type { IdentityProviderCapabilityRead } from "@gram/client/models/components/identityprovidercapabilityread.js";
import { Badge } from "@/components/ui/Badge";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";

interface CapabilityCopy {
  label: string;
  access: string;
  /** The sub-step that stops working without this capability. */
  usedBy: string;
  /**
   * The noun the count is of, where the resource name is not one a person
   * would read. Absent where the resource already says it, as `users` and
   * `groups` do.
   */
  counted?: string;
}

// What each capability means in the administrator's terms. The server names
// capabilities, not scopes, so this says what the capability buys rather than
// which Okta scope carries it.
const CAPABILITY_COPY: Record<string, CapabilityCopy> = {
  directory_read: {
    label: "People and group membership",
    access: "Read",
    usedBy: "Directory",
  },
  application_assignment_read: {
    label: "Application assignments",
    access: "Read",
    usedBy: "Applications and access",
  },
  sign_in_provisioning: {
    label: "Sign-in application",
    access: "Create",
    usedBy: "Single sign-on",
  },
  group_assignment: {
    label: "Group assignments",
    access: "Assign",
    usedBy: "Directory sync",
  },
  claims_provisioning: {
    label: "Sign-in claims",
    access: "Manage",
    usedBy: "Single sign-on",
  },
};

/**
 * Two reads can prove one capability, so where the resource is what tells them
 * apart it names the row. Sign-on is the case: creating the application is one
 * capability, read back in Okta and again in Speakeasy.
 */
const RESOURCE_COPY: Record<string, CapabilityCopy> = {
  sign_in_application: {
    label: "Sign-in application in Okta",
    access: "Create",
    usedBy: "Single sign-on",
  },
  sign_in_connection: {
    label: "Speakeasy's sign-in provider",
    access: "Create",
    usedBy: "Single sign-on",
  },
  // The directory check makes four reads across two capabilities, so the
  // resource is the only thing telling them apart: two rows would otherwise
  // both read "Group assignments" and two more "People and group membership".
  directory_application: {
    label: "Directory application in Okta",
    access: "Create",
    usedBy: "Directory sync",
  },
  directory_connection: {
    label: "Provisioning connection in Okta",
    access: "Read",
    usedBy: "Directory sync",
  },
  directory_groups: {
    label: "Groups arrived from Okta",
    access: "Read",
    usedBy: "Directory sync",
    counted: "groups",
  },
  directory_users: {
    label: "People arrived from Okta",
    access: "Read",
    usedBy: "Directory sync",
    counted: "people",
  },
};

function rowCopy(read: IdentityProviderCapabilityRead): CapabilityCopy {
  // A capability this build has no copy for is still worth showing: the read
  // either worked or it did not, and inventing a friendly name would be worse
  // than printing what the server called it.
  return (
    RESOURCE_COPY[read.resource] ??
    CAPABILITY_COPY[read.capability] ?? {
      label: read.capability,
      access: "—",
      usedBy: "—",
    }
  );
}

/** The badge: what came back, short enough to sit on one line. */
function readResult(read: IdentityProviderCapabilityRead): string {
  if (!read.ok) return "No answer";
  if (read.count === undefined) return "Active";
  // The resource is a key, and most of them happen to be the plural noun the
  // count is of. The ones that are not say so, rather than printing
  // "12 directory_groups" at an administrator.
  return `${read.count.toLocaleString()} ${rowCopy(read).counted ?? read.resource}`;
}

const columns: Column<IdentityProviderCapabilityRead>[] = [
  {
    key: "capability",
    header: "Capability",
    width: "2fr",
    render: (read) => (
      <Text className="font-medium">{rowCopy(read).label}</Text>
    ),
  },
  {
    key: "access",
    header: "Access",
    width: "1fr",
    render: (read) => <Text>{rowCopy(read).access}</Text>,
  },
  {
    key: "usedBy",
    header: "Used by",
    width: "1.5fr",
    render: (read) => <Text muted>{rowCopy(read).usedBy}</Text>,
  },
  {
    key: "result",
    header: "Last read",
    render: (read) => (
      <div className="space-y-1">
        <Badge variant={read.ok ? "success" : "warning"} background size="sm">
          <Badge.Text>{readResult(read)}</Badge.Text>
        </Badge>
        {/* The server's own words for what it saw, which for a read with no
            count to report is the only detail there is. */}
        {read.detail ? (
          <Text variant="small" muted>
            {read.detail}
          </Text>
        ) : null}
      </div>
    ),
  },
];

/**
 * What the connection proved it can do in Okta, one row per read the check
 * actually made. Shared by the verification result and the connected state so
 * the same evidence reads the same way in both places.
 */
export function IdentityProviderCapabilities({
  reads,
}: {
  reads: IdentityProviderCapabilityRead[];
}): JSX.Element | null {
  if (reads.length === 0) return null;

  return (
    <Table
      columns={columns}
      data={reads}
      rowKey={(read) => `${read.capability}:${read.resource}`}
    />
  );
}
