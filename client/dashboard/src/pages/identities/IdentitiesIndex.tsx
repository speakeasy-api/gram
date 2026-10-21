import { IdentitySyncCallout } from "@/components/setup-empty-state";
import {
  buildEmployees,
  type Employee,
  type EmployeeAccount,
} from "@/components/observe/insightsEmployeesData";
import { AccountRow } from "@/components/observe/account-display";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
import { Icon } from "@/components/ui/Icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Info } from "lucide-react";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { defineFilters, useFilterState } from "@/components/filters";
import {
  useOrganization,
  useSession,
  useIsPlatformAdmin,
} from "@/contexts/Auth";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useSdkClient, useProjectSlugForRequests } from "@/contexts/Sdk";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { AgentOwner } from "@/pages/agents/agent-admin";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { useRBAC } from "@/hooks/useRBAC";
import { Badge } from "@/components/ui/Badge";
import { Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { dateTimeFormatters } from "@/lib/dates";
import { useHideInsightsDock } from "@/components/insights-context";
import { Text } from "@/components/ui/Text";
import { IdentityLink } from "@/components/identity-link";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ActingPrincipal } from "@/components/ui/ActingPrincipal";
import { Bot, Plus, User } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, Navigate, Outlet, useLocation, useNavigate } from "react-router";
import { useCatalogEntries } from "@/pages/agents/credentials/workloadCatalog";
import { useWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import {
  identityKindOf,
  identityUrnForEmployee,
  IDENTITY_KIND_LABELS,
} from "./identityKind";
import {
  coverageForIdentity,
  deviceCoverageQueryKey,
  fetchDeviceCoverage,
  NO_DEVICE_BUCKET,
} from "./identityDeviceCoverage";
import type { AgentLifecycleFilter } from "./identityRoster";
import {
  fetchIdentityRoster,
  identityRosterQueryKey,
  fetchRegisteredAgents,
  registeredAgentIdentity,
  matchesIdentityTelemetryFilters,
} from "./identityRoster";

export function IdentitiesRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * The bare detail URL has no content of its own — every panel lives on a tab —
 * so send it to the overview rather than render a header over an empty pane.
 * Hand-typed and truncated links land here.
 */
export function IdentityDetailIndexRedirect(): JSX.Element {
  // Built from the path rather than from the route helper and a param: the
  // param is empty here, so the helper produced /identities/overview — a
  // dead address — and the breadcrumb to an identity 404'd.
  const location = useLocation();
  return (
    <Navigate
      to={`${location.pathname.replace(/\/$/, "")}/overview${location.search}`}
      replace
    />
  );
}

/** Reads `?sort=<column>:<asc|desc>`, ignoring anything it does not name. */
function parseSortParam(search: string): SortDescriptor | null {
  const raw = new URLSearchParams(search).get("sort");
  if (!raw) return null;
  const [id, direction] = raw.split(":");
  if (!id || !IDENTITY_COLUMNS.some((column) => column.key === id)) return null;
  return { id, direction: direction === "asc" ? "asc" : "desc" };
}

// The roster is one merged list held in memory, so it pages here rather than
// at either source.
const PAGE_SIZE = 50;

const IDENTITY_FILTERS = defineFilters([
  {
    id: "kind",
    label: "Kind",
    kind: "multiselect",
    pinned: true,
    description: "People and agents.",
  },
  // Default-on, and inverted for it: the absent value is the filtered one,
  // because a URL with no filters still has to arrive excluded. The roster
  // is mostly these rows, and a page that opens full of opaque hashes reads
  // as broken rather than as complete.
  {
    id: "anonymous",
    label: "Anonymous",
    kind: "select",
    pinned: true,
    // No clear: emptying it resolves straight back to excluding them, so a ×
    // would look like it did nothing.
    required: true,
    allLabel: "Anonymous excluded",
    description:
      "Activity whose only identifier is an opaque client id, never matched to a person. It arrives from sessions signed in with personal credentials, and carries no name, no account and no tokens.",
  },
  // Three separate facts, each its own filter. None implies another: a
  // directory profile can belong to someone who never signed in, and activity
  // can come from someone with no account here.
  {
    id: "directory",
    label: "Directory",
    kind: "select",
    allLabel: "All",
    description:
      "Whether the identity has a live profile in the organization's synced directory.",
  },
  {
    id: "membership",
    label: "Membership",
    kind: "select",
    allLabel: "All",
    description: "Whether the identity is a member of this organization.",
  },
  {
    id: "seen",
    label: "Seen",
    kind: "select",
    allLabel: "All",
    description:
      "Whether activity has ever been observed for the identity through hooks or MCP.",
  },
  {
    id: "activity",
    label: "Last activity",
    kind: "select",
    allLabel: "Any time",
    description: "How recently the identity was last seen working.",
  },
  {
    id: "device_status",
    label: "Device agent",
    kind: "multiselect",
    description:
      "Agent coverage on the identity's managed devices, as the MDM inventory reports it. An identity with several machines is described by its best one.",
  },
  {
    id: "role",
    label: "Roles",
    kind: "multiselect",
    description: "Roles assigned in this organization.",
  },
  {
    id: "department",
    label: "Department",
    kind: "multiselect",
    description: "Department as reported by the connected identity provider.",
  },
  {
    id: "team",
    label: "Team",
    kind: "multiselect",
    description:
      "Directory groups the identity belongs to, from the connected identity provider.",
  },
  {
    id: "account_type",
    label: "Account type",
    kind: "select",
    allLabel: "All",
    description: "Usage on personal accounts versus team-managed ones.",
  },
  {
    id: "personal_account",
    label: "Uses personal account",
    kind: "select",
    allLabel: "All",
    description:
      "Identities holding at least one personal account, or none at all.",
  },
]);

/**
 * An agent is not a person: it has no department, no directory team, no
 * managed device and no AI-provider account. Filtering it by those asks
 * questions none of its rows can answer, so the agents page gets the
 * dimensions an agent actually has — all three columns on the agent itself,
 * and all three applied by the server that pages the list.
 */
const AGENT_FILTERS = defineFilters([
  {
    id: "lifecycle",
    label: "Status",
    kind: "multiselect",
    pinned: true,
    description: "Whether the agent can still act, and why not if it cannot.",
  },
  {
    id: "owner",
    label: "Owner",
    kind: "multiselect",
    description:
      "The person accountable for the agent, who holds it even while others hold grants on it.",
  },
  {
    id: "registered",
    label: "Registered",
    kind: "select",
    allLabel: "Any time",
    description: "When the agent identity was created here.",
  },
]);

const AGENT_LIFECYCLE_OPTIONS = [
  { value: "active", label: "Active" },
  { value: "suspended", label: "Suspended" },
  { value: "revoked", label: "Revoked" },
];

/** Each bucket is the window it names, matching the people roster's. */
const AGENT_REGISTERED_OPTIONS = [
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "older", label: "Over 30 days ago" },
];

/** The cutoffs a registered bucket means, as the server reads them. */
function registeredWindow(bucket: string): {
  after?: Date;
  before?: Date;
} {
  const days = (count: number) =>
    new Date(Date.now() - count * 24 * 60 * 60 * 1000);
  switch (bucket) {
    case "7d":
      return { after: days(7) };
    case "30d":
      return { after: days(30) };
    case "older":
      return { before: days(30) };
    default:
      return {};
  }
}

const ACCOUNT_TYPE_OPTIONS = [
  { value: "personal", label: "Personal" },
  { value: "team", label: "Team" },
];

// A chip carries the chosen option's label and nothing else, so each one has
// to name the filter it came from: a chip reading "Yes" says nothing about
// what was answered.
const PERSONAL_ACCOUNT_OPTIONS = [
  { value: "yes", label: "Personal account" },
  { value: "no", label: "No personal account" },
];

const DIRECTORY_OPTIONS = [
  { value: "in_directory", label: "In directory" },
  { value: "not_in_directory", label: "Not in directory" },
];

/**
 * Only the opt-in. "Excluded" is the dimension's own all-label, so it is the
 * value a page with no filters already holds rather than a second option
 * saying the same thing.
 */
const ANONYMOUS_OPTIONS = [{ value: "included", label: "Anonymous included" }];

const MEMBERSHIP_OPTIONS = [
  { value: "member", label: "Member" },
  { value: "not_member", label: "Not a member" },
];

const SEEN_OPTIONS = [
  { value: "seen", label: "Seen" },
  { value: "not_seen", label: "Not seen" },
];

/**
 * Activity buckets, each the window it names — "Last 30 days" includes the
 * last week rather than excluding it, because a reader narrowing to a month
 * means "this month's people", not "the ones who went quiet mid-month".
 */
const ACTIVITY_OPTIONS = [
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "older", label: "Over 30 days ago" },
  { value: "never", label: "Never active" },
];

/** How each device-coverage bucket reads in the filter. */
const DEVICE_STATUS_LABELS: Record<string, string> = {
  agent_active: "Agent active",
  agent_stale: "Agent stale",
  agent_other_device: "Agent on another device",
  no_agent: "No agent",
  no_email: "No assigned email",
  unresolved_email: "Email unresolved",
  missing: "Missing from MDM",
  [NO_DEVICE_BUCKET]: "No managed device",
};

const KIND_OPTIONS = [
  { value: "", label: "All" },
  {
    value: "person",
    label: (
      <span className="flex items-center gap-2">
        <User className="size-4" aria-hidden="true" />
        People
      </span>
    ),
  },
  {
    value: "agent",
    label: (
      <span className="flex items-center gap-2">
        <Bot className="size-4" aria-hidden="true" />
        Agents
      </span>
    ),
  },
];

// One em dash for every kind of "no role": an agent has none by definition, a
// person with no account has none yet, and the roster reports an absent role
// as a bare "-" or "Unknown" depending on which source answered. Three
// spellings of nothing in one column read as three different states.
/** The distinct non-empty values in a column, as sorted filter options. */
function sortedValueOptions(
  values: string[],
): { value: string; label: string }[] {
  return [...new Set(values.filter((value) => value !== ""))]
    .sort((a, b) => a.localeCompare(b))
    .map((value) => ({ value, label: value }));
}

function roleLabel(identity: Employee): string {
  if (identityKindOf(identity) !== "person") return "\u2014";
  const role = identity.role.trim();
  if (!role || role === "-" || role === "Unknown") return "\u2014";
  return role;
}

const IDENTITY_COLUMNS: Column<Employee>[] = [
  {
    key: "identity",
    header: "Identity",
    // The name and address are the widest thing in the row and the thing the
    // row is looked up by, so the flexible space goes here rather than being
    // shared out evenly with columns holding a word or a number.
    width: "2.4fr",
    sortable: true,
    sortValue: (identity) => identity.name.toLowerCase(),
    render: (identity) => <IdentityCell identity={identity} />,
  },
  {
    key: "kind",
    header: "Kind",
    width: "140px",
    sortable: true,
    sortValue: (identity) => IDENTITY_KIND_LABELS[identityKindOf(identity)],
    render: (identity) => {
      const kind = identityKindOf(identity);
      return (
        <Badge variant={kind === "agent" ? "information" : "neutral"}>
          {IDENTITY_KIND_LABELS[kind]}
        </Badge>
      );
    },
  },
  {
    key: "role",
    header: "Roles",
    width: "1.3fr",
    render: (identity) => (
      <Text muted small className="truncate">
        {roleLabel(identity)}
      </Text>
    ),
  },
  {
    key: "accounts",
    header: (
      <span className="flex items-center gap-1">
        Accounts
        <SimpleTooltip
          tooltip={`The AI provider accounts (Claude, Codex, Cursor) each identity has been seen using, labelled team or personal. Accounts are linked automatically from tool activity, so this stays blank until an identity is seen using a recognized account. ${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}`}
        >
          <Info className="text-muted-foreground size-3 shrink-0" />
        </SimpleTooltip>
      </span>
    ),
    // "2 accounts" plus its chevron, not a share of the leftover space.
    width: "150px",
    sortable: true,
    sortLabel: "Accounts",
    // Personal-holders first (ascending), then more accounts before fewer, so
    // the rows worth a second look group at the top.
    sortValue: (identity) =>
      (identity.hasPersonalAccount ? 0 : 1_000_000) - identity.accounts.length,
    render: (identity) => <AccountsCell identity={identity} />,
  },
  {
    key: "lastActivity",
    header: "Last activity",
    width: "180px",
    sortable: true,
    sortValue: (identity) => identity.lastActivityTimestamp ?? 0,
    render: (identity) => <LastActivityCell identity={identity} />,
  },
  {
    key: "tokens",
    header: "Tokens",
    width: "130px",
    sortable: true,
    sortValue: (identity) => identity.tokenCount,
    render: (identity) => (
      <Text small className="tabular-nums">
        {identity.tokenCount.toLocaleString()}
      </Text>
    ),
  },
];

/**
 * People and agents are two rosters, not two halves of one: a person arrives
 * from the identity provider and cannot be created here, while an agent is
 * registered here and issued its key here. Each gets its own page, and each
 * page gets the whole screen.
 */
export type RosterKind = "person" | "agent";

export default function IdentitiesIndex({
  kind,
}: {
  kind: RosterKind;
}): JSX.Element {
  const page = (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <IdentitiesIndexContent kind={kind} />
      </Page.Body>
    </Page>
  );

  // The people roster is built from project telemetry, so it needs the
  // project. An agent is authorized by who owns it, which the server decides
  // per agent — gating the page on project:read would shut someone out of
  // agents that are theirs.
  if (kind === "person") {
    return (
      <RequireScope scope={["project:read"]} level="page">
        {page}
      </RequireScope>
    );
  }

  return page;
}

/** The bare /identities URL lands on the people roster. */
export function PeopleIndexRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.identities.people.href()}${location.search}`}
      replace
    />
  );
}

export function PeopleIndex(): JSX.Element {
  return <IdentitiesIndex kind="person" />;
}

export function AgentsIndex(): JSX.Element {
  return <IdentitiesIndex kind="agent" />;
}

function IdentitiesIndexContent({ kind }: { kind: RosterKind }): JSX.Element {
  // This page fills the viewport, so the floating dock sat on top of the
  // agents table rather than beside it.
  useHideInsightsDock();
  const location = useLocation();
  const routes = useRoutes();
  const organization = useOrganization();
  const projectSlug = useProjectSlugForRequests();
  const navigate = useNavigate();
  const client = useGramContext();
  const sdk = useSdkClient();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { hasScope } = useRBAC();
  const canReadOrganization = hasScope("org:read", organization.id);
  // Match the existing agent screen's supported sessions, without changing scope.
  const agentsEnabled =
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) === null;
  const [search, setSearch] = useState("");
  // Honours `?sort=<column>:<asc|desc>` so a handoff can open the list on the
  // order it was talking about — the dashboard's Top Users "View all" means
  // "these people, by tokens", and landing on last-activity order shows a
  // different set entirely.
  // A roster is read by name, so it opens in name order. "Who was busy
  // lately" is a question the sort control answers on request, and one that
  // `?sort=` still opens the page on when a handoff asks for it.
  const [sort, setSort] = useState<SortDescriptor | null>(
    parseSortParam(location.search) ?? {
      id: "identity",
      direction: "asc",
    },
  );
  // Each roster filters on its own dimensions, so each gets its own schema.
  const { values, setValue, clearValue, clearAll } = useFilterState(
    kind === "agent" ? AGENT_FILTERS : IDENTITY_FILTERS,
  );
  const lifecycleKey = JSON.stringify(values.lifecycle ?? []);
  const ownerKey = JSON.stringify(values.owner ?? []);
  const registered = (values.registered as string | undefined) ?? "";

  // The agents table sorts on its own column, by its own order: the people
  // table's "last activity, descending" names a column agents do not have.
  // Name is the only agent column that sorts, so this is a direction.
  const [agentSort, setAgentSort] = useState<SortDescriptor | null>({
    id: "identity",
    direction: "asc",
  });
  const agentSortOrder = agentSort?.direction === "desc" ? "desc" : "asc";
  // The agent roster pages at the server: the cursor walks the same ordering
  // and the same search the query ran, so a page is a page of the list the
  // reader is actually looking at.
  //
  // Only the agents page reads it. The people table drops agent rows anyway,
  // so fetching them there would be a request whose every row is discarded.
  const agentsQuery = useInfiniteQuery({
    queryKey: [
      "identities",
      "registered-agents",
      organization.id,
      session.user.id,
      search.trim().toLowerCase(),
      agentSortOrder,
      lifecycleKey,
      ownerKey,
      registered,
    ],
    queryFn: ({ pageParam, signal }) =>
      fetchRegisteredAgents(
        sdk,
        {
          cursor: pageParam,
          limit: PAGE_SIZE,
          search,
          sortOrder: agentSortOrder,
          lifecycle: JSON.parse(lifecycleKey) as AgentLifecycleFilter[],
          ownerUserIds: JSON.parse(ownerKey) as string[],
          ...registeredWindow(registered),
        },
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: agentsEnabled && kind === "agent",
    throwOnError: false,
    retry: false,
  });
  const registeredAgents = useMemo(
    () => (agentsQuery.data?.pages ?? []).flatMap((page) => page.items),
    [agentsQuery.data],
  );

  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  // Usage is the only project-scoped read here, and it is what surfaces the
  // identities the directory has never heard of.
  //
  // Unwindowed on purpose: this is the roster, not a report. Someone who has
  // been quiet for a quarter is still an identity, and hiding them behind a
  // date range would make the list answer "who was active lately" — a question
  // the per-identity pages already answer, each over its own window.
  const usageQuery = useQuery({
    queryKey: identityRosterQueryKey(organization.id, projectSlug),
    queryFn: () => fetchIdentityRoster(client, projectSlug),
    throwOnError: false,
  });
  // The MDM fleet, read once and folded to a bucket per identity. It is not
  // part of the roster: an org with no device integration connected simply
  // has no fleet, and the device filter drops out rather than offering a
  // dimension where every identity answers the same.
  const deviceCoverageQuery = useQuery({
    queryKey: deviceCoverageQueryKey(organization.id),
    queryFn: () => fetchDeviceCoverage(client),
    enabled: canReadOrganization,
    throwOnError: false,
  });
  const deviceCoverage = canReadOrganization
    ? deviceCoverageQuery.data
    : undefined;

  // The list is the join of the roster reads, so until they land there is no
  // roster to report on — and "0 identities" or "No identities match these
  // filters" is a statement about the organization, not about a request still
  // in flight or one that never came back.
  const rosterLoading =
    membersQuery.isLoading ||
    rolesQuery.isLoading ||
    usageQuery.isLoading ||
    (agentsEnabled && agentsQuery.isLoading);
  const rosterFailed =
    membersQuery.isError ||
    rolesQuery.isError ||
    usageQuery.isError ||
    (agentsEnabled && agentsQuery.isError);
  const retryRoster = () => {
    if (membersQuery.isError) void membersQuery.refetch();
    if (rolesQuery.isError) void rolesQuery.refetch();
    if (usageQuery.isError) void usageQuery.refetch();
    if (agentsEnabled && agentsQuery.isError) void agentsQuery.refetch();
  };

  const identities = useMemo(
    () => [
      ...buildEmployees(
        membersQuery.data?.members ?? [],
        rolesQuery.data?.roles ?? [],
        usageQuery.data ?? [],
      ),
      ...(agentsEnabled ? registeredAgents : []).map(registeredAgentIdentity),
    ],
    [
      membersQuery.data,
      rolesQuery.data,
      usageQuery.data,
      agentsEnabled,
      registeredAgents,
    ],
  );

  // The sync callout needs the people count, and the directory filter needs
  // to know whether this org has a directory behind it.
  const counts = useMemo(() => {
    const tally = { people: 0, inDirectory: 0 };
    for (const identity of identities) {
      if (identityKindOf(identity) === "person") tally.people += 1;
      if (identity.inDirectory) tally.inDirectory += 1;
    }
    return tally;
  }, [identities]);

  const kindKey = (values.kind ?? []).join(",");
  const hasLegacyKindFilter =
    !!kindKey && !KIND_OPTIONS.some((option) => option.value === kindKey);

  const kindOptions = [...KIND_OPTIONS];
  if (hasLegacyKindFilter)
    kindOptions.push({ value: kindKey, label: "Custom" });

  // Option lists the data decides: a role the org never assigned, a department
  // nobody is in, or a device bucket no machine falls into is a filter that can
  // only empty the table, so each dimension offers what the roster actually
  // holds and drops out entirely when it holds nothing.
  const roleOptions = useMemo(() => {
    const assigned = new Set(
      identities.flatMap((identity) => identity.roleIds),
    );
    return (rolesQuery.data?.roles ?? [])
      .filter((role) => assigned.has(role.id))
      .map((role) => ({ value: role.id, label: role.name }));
  }, [identities, rolesQuery.data]);

  const departmentOptions = useMemo(
    () => sortedValueOptions(identities.map((identity) => identity.department)),
    [identities],
  );
  const teamOptions = useMemo(
    () => sortedValueOptions(identities.flatMap((identity) => identity.teams)),
    [identities],
  );
  const deviceStatusOptions = useMemo(() => {
    // A capped walk leaves every identity past the cap looking like it owns no
    // machine, which the filter would report as fact. Offer nothing rather than
    // a dimension that misfiles the fleet's tail.
    if (!deviceCoverage || deviceCoverage.truncated) return [];
    if (deviceCoverage.deviceCount === 0) return [];
    const present = new Set(
      identities.map((identity) =>
        coverageForIdentity(deviceCoverage, identity),
      ),
    );
    return [...present]
      .map((bucket) => ({
        value: bucket,
        label: DEVICE_STATUS_LABELS[bucket] ?? bucket,
      }))
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [identities, deviceCoverage]);

  // A dimension with nothing to offer would render an empty control; leave it
  // out of the schema rather than show a filter that cannot filter. A dimension
  // whose value is still set in the URL is kept regardless of its options: it is
  // narrowing the list, and dropping its control would leave no way to clear it.
  const ownerOptions = useMemo(
    () =>
      (membersQuery.data?.members ?? [])
        .map((member) => ({ value: member.id, label: member.name }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    [membersQuery.data],
  );
  const filterSchema = useMemo(
    () =>
      kind === "agent"
        ? AGENT_FILTERS.filter((dimension) => {
            const selected = (values[dimension.id as keyof typeof values] ??
              []) as string[];
            if (selected.length > 0) return true;
            // An owner list with nobody in it is a control that cannot narrow.
            if (dimension.id === "owner") return ownerOptions.length > 0;
            return true;
          })
        : IDENTITY_FILTERS.filter((dimension) => {
            if (dimension.id === "kind") return hasLegacyKindFilter;
            const selected = (values[dimension.id as keyof typeof values] ??
              []) as string[];
            if (selected.length > 0) return true;
            if (dimension.id === "role") return roleOptions.length > 0;
            if (dimension.id === "department")
              return departmentOptions.length > 0;
            if (dimension.id === "team") return teamOptions.length > 0;
            if (dimension.id === "device_status") {
              return deviceStatusOptions.length > 0;
            }
            // With no directory connected every identity answers "not in
            // directory", so the filter could only empty the table or do
            // nothing.
            if (dimension.id === "directory") return counts.inDirectory > 0;
            return true;
          }),
    [
      kind,
      values,
      counts.inDirectory,
      hasLegacyKindFilter,
      ownerOptions.length,
      roleOptions.length,
      departmentOptions.length,
      teamOptions.length,
      deviceStatusOptions.length,
    ],
  );

  const accountType = (values.account_type as string | undefined) ?? "";
  const personalAccount = (values.personal_account as string | undefined) ?? "";
  // Absent means excluded: this filter is on before anyone touches it.
  const anonymous = (values.anonymous as string | undefined) ?? "";
  const directory = (values.directory as string | undefined) ?? "";
  const membership = (values.membership as string | undefined) ?? "";
  const seen = (values.seen as string | undefined) ?? "";
  const activity = (values.activity as string | undefined) ?? "";
  // Department and team hold whatever the directory calls them, so these keys
  // are JSON rather than a comma join: a department named "Sales, EMEA" would
  // split back into two values that match nobody.
  const deviceStatusKey = JSON.stringify(values.device_status ?? []);
  const roleKey = JSON.stringify(values.role ?? []);
  const departmentKey = JSON.stringify(values.department ?? []);
  const teamKey = JSON.stringify(values.team ?? []);
  const rows = useMemo(() => {
    const selectedKinds = kindKey ? kindKey.split(",") : [];
    const selectedDeviceStatuses = JSON.parse(deviceStatusKey) as string[];
    const selectedRoles = JSON.parse(roleKey) as string[];
    const selectedDepartments = JSON.parse(departmentKey) as string[];
    const selectedTeams = JSON.parse(teamKey) as string[];
    const query = search.trim().toLowerCase();
    return identities.filter((identity) => {
      // The agents page asked the server for exactly this list — its status,
      // owner, registration window and name search are all in the query that
      // produced the page. Re-applying people predicates here would drop rows
      // for having no department.
      if (kind === "agent") return true;
      // An identity with nothing but an opaque client id behind it. Shown
      // only when asked for: it is a record of activity, not of a person,
      // and it crowds out every row that names someone.
      //
      // Naming the kind is asking for them, so it beats the default. A link
      // written to show exactly these rows must not land on an empty table.
      if (
        anonymous !== "included" &&
        !selectedKinds.includes("unknown") &&
        identityKindOf(identity) === "unknown"
      ) {
        return false;
      }
      if (
        selectedKinds.length > 0 &&
        !selectedKinds.includes(identityKindOf(identity))
      ) {
        return false;
      }
      if (
        !matchesIdentityTelemetryFilters(
          identity,
          { directory, membership, seen },
          activity,
        )
      )
        return false;
      if (
        selectedDeviceStatuses.length > 0 &&
        !selectedDeviceStatuses.includes(
          coverageForIdentity(deviceCoverage, identity),
        )
      ) {
        return false;
      }
      if (
        selectedRoles.length > 0 &&
        !identity.roleIds.some((id) => selectedRoles.includes(id))
      ) {
        return false;
      }
      if (
        selectedDepartments.length > 0 &&
        !selectedDepartments.includes(identity.department)
      ) {
        return false;
      }
      if (
        selectedTeams.length > 0 &&
        !identity.teams.some((team) => selectedTeams.includes(team))
      ) {
        return false;
      }
      // Each value matches an identity holding at least one account of that
      // type; someone with both a team and a personal account shows under
      // either.
      if (
        accountType &&
        !identity.accounts.some((a) => a.accountType === accountType)
      ) {
        return false;
      }
      if (
        personalAccount &&
        identity.hasPersonalAccount !== (personalAccount === "yes")
      ) {
        return false;
      }
      if (!query) return true;
      return (
        identity.name.toLowerCase().includes(query) ||
        identity.email.toLowerCase().includes(query)
      );
    });
  }, [
    kind,
    identities,
    search,
    kindKey,
    anonymous,
    accountType,
    personalAccount,
    directory,
    membership,
    seen,
    activity,
    deviceStatusKey,
    deviceCoverage,
    roleKey,
    departmentKey,
    teamKey,
  ]);

  const sortedRows = useMemo(
    () => sortTableData(rows, IDENTITY_COLUMNS, sort) as Employee[],
    [rows, sort],
  );
  // Any change to what is being listed starts the list over: keeping a deep
  // scroll position across a new filter shows the reader page four of
  // something they have not seen page one of.
  const agentsById = useMemo(
    () => new Map(registeredAgents.map((agent) => [agent.id, agent])),
    [registeredAgents],
  );
  // How each agent signs in. Only the agents roster shows it, and both reads
  // are workload-policy requests: asking for them on the people roster, or on
  // a roster this organization is not shown at all, is a request whose every
  // answer is discarded, over a wider authorization surface than the page
  // needs.
  const agentPlatformsShown = agentsEnabled && kind === "agent";
  const catalog = useCatalogEntries(agentPlatformsShown);
  const workloads = useWorkloadIdentities({}, undefined, {
    enabled: agentPlatformsShown,
    throwOnError: false,
  });
  // An agent can be admitted from more than one platform, so each row keeps
  // every one of them: a map of one admission per agent reported whichever
  // happened to come last.
  const platformsByAgent = useMemo(() => {
    const byAgent = new Map<string, string[]>();
    for (const admission of workloads.data?.admissions ?? []) {
      if (admission.agentId === "") continue;
      // A wildcard rule the platform has not enabled admits nothing, so no
      // sign-in happens through it. The column answers "how does this agent
      // sign in", and a dormant rule is not an answer; the agent's own page
      // flags it, which a 160px column of platform names cannot.
      if (admission.matchKind === "wildcard" && !admission.wildcardActive) {
        continue;
      }
      // A catalog platform by its own name; anything else was registered by
      // hand and carries the label its operator gave it. "Custom" is the
      // fallback for one that was left unnamed, not the name of all of them.
      const known = catalog.entries.find(
        (entry) => entry.issuer.value === admission.issuer,
      );
      const label =
        known?.displayName ?? (admission.issuerName.trim() || "Custom");
      const names = byAgent.get(admission.agentId);
      if (names === undefined) byAgent.set(admission.agentId, [label]);
      else if (!names.includes(label)) names.push(label);
    }
    return byAgent;
  }, [workloads.data, catalog.entries]);
  // An agent has no roles, no linked accounts and no inbox: those columns were
  // an em dash on every row. What it does have is a lifecycle, an owner and a
  // date it was registered, which is what someone scanning this table wants.
  const agentColumns = useMemo<Column<Employee>[]>(
    () => [
      {
        key: "identity",
        header: "Agent",
        width: "1.6fr",
        sortable: true,
        sortValue: (identity) => identity.name.toLowerCase(),
        render: (identity) => <IdentityCell identity={identity} />,
      },
      {
        key: "platform",
        header: "Platform",
        width: "160px",
        render: (identity) => {
          // "API key" means "this agent holds no admission", which only a
          // read that came back can say. While it is in flight, or after it
          // fails, the column would otherwise call every federated agent
          // key-authenticated.
          if (!workloads.isSuccess) {
            return (
              <Text muted small className="truncate">
                {workloads.isError ? "Unknown" : "—"}
              </Text>
            );
          }
          const platforms = platformsByAgent.get(
            identity.registeredAgentId ?? "",
          );
          if (platforms === undefined) {
            return (
              <Text muted small className="truncate">
                API key
              </Text>
            );
          }
          // One platform by name. Several is a fact about the agent, so the
          // column says how many rather than picking one of them.
          return (
            <Text small className="truncate" title={platforms.join(", ")}>
              {platforms.length === 1
                ? platforms[0]
                : `${platforms.length} platforms`}
            </Text>
          );
        },
      },
      {
        key: "status",
        header: "Status",
        width: "140px",
        render: (identity) => {
          const agent = agentsById.get(identity.registeredAgentId ?? "");
          if (!agent) return null;
          return (
            <Badge
              size="sm"
              variant={
                agent.lifecycle === "active"
                  ? "success"
                  : agent.lifecycle === "revoked"
                    ? "destructive"
                    : "warning"
              }
            >
              {agent.lifecycle}
            </Badge>
          );
        },
      },
      {
        key: "owner",
        header: "Owner",
        width: "1fr",
        render: (identity) => {
          const agent = agentsById.get(identity.registeredAgentId ?? "");
          if (!agent) {
            return (
              <Text muted small className="truncate">
                —
              </Text>
            );
          }
          // The same face the agent's own page shows, so the owner reads as
          // one person across both: answering "who runs this agent" usually
          // means going to look at them.
          return <AgentOwner agent={agent} />;
        },
      },
      {
        key: "created",
        header: "Registered",
        width: "160px",
        render: (identity) => {
          const created = agentsById.get(
            identity.registeredAgentId ?? "",
          )?.createdAt;
          return created ? (
            <time
              className="tabular-nums"
              dateTime={created.toISOString()}
              title={dateTimeFormatters.full.format(created)}
            >
              {dateTimeFormatters.day.format(created)}
            </time>
          ) : (
            "—"
          );
        },
      },
      {
        key: "actions",
        header: "Actions",
        width: "160px",
        render: (identity) => {
          const agentId = identity.registeredAgentId ?? "";
          if (agentId === "") return null;
          return (
            // Stops the row's own click from firing underneath: the row opens
            // the agent, and Edit is a different destination.
            <div
              className="flex items-center justify-end gap-2"
              onClick={(event) => event.stopPropagation()}
            >
              <Button asChild size="sm" variant="primary">
                <Link to={routes.identities.agents.edit.href(agentId)}>
                  Edit
                </Link>
              </Button>
              <Button asChild size="sm" variant="secondary">
                <Link
                  to={routes.identities.detail.overview.href(
                    encodeIdentityUrn(identityUrnForEmployee(identity)),
                  )}
                >
                  View
                </Link>
              </Button>
            </div>
          );
        },
      },
    ],
    [
      agentsById,
      platformsByAgent,
      workloads.isSuccess,
      workloads.isError,
      routes,
    ],
  );
  // One sorted list, split by what the row is. The filters above still narrow
  // both; the Kind column is dropped inside each table because the heading
  // already says it.
  const peopleRows = useMemo(
    () => sortedRows.filter((row) => identityKindOf(row) !== "agent"),
    [sortedRows],
  );
  // The server already returned these in the order and the search the query
  // asked for, so they are not re-sorted here: sorting the loaded pages would
  // only reorder the rows that happen to have arrived.
  const agentRows = useMemo(
    () => sortedRows.filter((row) => identityKindOf(row) === "agent"),
    [sortedRows],
  );
  const [peopleVisible, setPeopleVisible] = useState(PAGE_SIZE);
  const openIdentity = (row: Employee) =>
    void navigate(
      routes.identities.detail.overview.href(
        encodeIdentityUrn(identityUrnForEmployee(row)),
      ),
    );
  const rosterMessage = (empty: string) =>
    rosterLoading ? (
      "Loading identities…"
    ) : rosterFailed ? (
      <span>
        The identity roster could not be loaded.{" "}
        <button
          type="button"
          onClick={retryRoster}
          className="underline underline-offset-2"
        >
          Try again
        </button>
      </span>
    ) : (
      empty
    );

  // A changed filter restarts both lists: paging is per table, the filters
  // are not.
  useEffect(() => {
    setPeopleVisible(PAGE_SIZE);
  }, [
    search,
    kindKey,
    anonymous,
    accountType,
    personalAccount,
    directory,
    membership,
    seen,
    activity,
    deviceStatusKey,
    roleKey,
    departmentKey,
    teamKey,
    sort,
  ]);

  return (
    <Page.Section>
      <Page.Section.Title>
        {kind === "agent" ? "Agents" : "People"}
      </Page.Section.Title>
      <Page.Section.Description className="max-w-2xl pr-8">
        {kind === "agent"
          ? "Register an agent identity for each agent running on an external platform. What it does through Speakeasy is then attributed to it."
          : "The people your identity provider knows about, and what each of them reached through Speakeasy. Speakeasy does not create them."}
      </Page.Section.Description>
      <Page.Section.CTA>
        {kind === "agent" && agentsEnabled ? (
          <div className="flex items-center gap-2">
            {/* The one platform with a guided setup, offered by name: it is
                what most people come to this page to do, and the generic
                flow makes them find it behind two more choices. */}
            <Button asChild variant="secondary">
              <Link
                to={`${routes.identities.agents.new.href()}?platform=claude-tag`}
              >
                <img
                  src="/access-hub/claude-tag.svg"
                  alt=""
                  className="size-4"
                />
                Set up Claude Tag
              </Link>
            </Button>
            <Button asChild variant="primary">
              <Link to={routes.identities.agents.new.href()}>
                <Plus className="size-4" aria-hidden="true" />
                New agent identity
              </Link>
            </Button>
          </div>
        ) : null}
      </Page.Section.CTA>
      <Page.Section.Body>
        {/* The section stacks its body children at 8px, which reads as one
            block: the tiles, the controls and the table are three things. */}
        {/* Directory sync is how people arrive; agents are registered here,
            so this has nothing to offer the agents roster. */}
        {kind === "person" &&
          !rosterLoading &&
          !rosterFailed &&
          counts.people <= 1 && <IdentitySyncCallout />}
        {/* The table is as tall as its rows, up to the room left under the
            controls. Past that it scrolls itself rather than the page: a short
            roster no longer leaves a band of empty table under the last row. */}
        {/* The banner-offset comes off the room as well: an impersonation or
            demo banner pushes everything below it down, and without this the
            pane kept asking for the height it had before the banner and put
            the page back on a scrollbar. */}
        <div className="flex max-h-[calc(100dvh-19rem-var(--banner-offset,0px))] flex-col gap-6">
          {/* No stat tiles: each table states its own count, and the four
              numbers above them repeated it without saying anything the rows
              do not. */}
          {/* Tucked under the description and trimmed down: the controls are
              a bar above the tables, not a band of their own. */}
          <div className="-mt-3 [&>*]:p-1.5 [&_input]:h-8">
            <Page.Toolbar>
              <Page.Toolbar.Search
                value={search}
                onChange={setSearch}
                placeholder={
                  kind === "agent" ? "Search agents…" : "Search people…"
                }
                debounceMs={200}
              />
              <Page.Toolbar.Actions>
                <Page.Toolbar.Filters
                  schema={filterSchema}
                  values={values}
                  optionsById={{
                    lifecycle: AGENT_LIFECYCLE_OPTIONS,
                    owner: ownerOptions,
                    registered: AGENT_REGISTERED_OPTIONS,
                    kind: Object.entries(IDENTITY_KIND_LABELS).map(
                      ([value, label]) => ({ value, label }),
                    ),
                    directory: DIRECTORY_OPTIONS,
                    anonymous: ANONYMOUS_OPTIONS,
                    membership: MEMBERSHIP_OPTIONS,
                    seen: SEEN_OPTIONS,
                    activity: ACTIVITY_OPTIONS,
                    device_status: deviceStatusOptions,
                    role: roleOptions,
                    department: departmentOptions,
                    team: teamOptions,
                    account_type: ACCOUNT_TYPE_OPTIONS,
                    personal_account: PERSONAL_ACCOUNT_OPTIONS,
                  }}
                  onChange={setValue as (id: string, value: unknown) => void}
                  onClear={clearValue as (id: string) => void}
                  onClearAll={clearAll}
                />
              </Page.Toolbar.Actions>
            </Page.Toolbar>
          </div>
          {/* One roster per page: a person arrives from the identity
              provider and cannot be created here, while an agent is
              registered here and issued its key here. Two jobs, two screens. */}
          {kind === "person" && (
            <IdentityGroup
              heading=""
              count={peopleRows.length}
              note=""
              columns={IDENTITY_COLUMNS.filter(
                (column) => column.key !== "kind",
              )}
              rows={peopleRows}
              visible={peopleVisible}
              onLoadMore={() => setPeopleVisible((count) => count + PAGE_SIZE)}
              sort={sort}
              onSortChange={setSort}
              onRowClick={openIdentity}
              emptyMessage={rosterMessage("No people match these filters")}
            />
          )}
          {/* The other roster is a sibling page with no link from this one,
              so someone looking for an agent here has nowhere to go. */}
          {kind === "person" && agentsEnabled && (
            <div className="border-border bg-card space-y-1.5 border p-4">
              <Text className="font-medium">Looking for agents?</Text>
              <Text muted small>
                Visit the{" "}
                <Link
                  to={routes.identities.agents.href()}
                  className="underline underline-offset-2"
                >
                  Agents
                </Link>{" "}
                page to see all agent identities configured on the platform, and
                set up new ones.
              </Text>
            </div>
          )}
          {kind === "agent" && (
            <IdentityGroup
              heading=""
              count={agentRows.length}
              note=""
              columns={agentColumns}
              rows={agentRows}
              sort={agentSort}
              onSortChange={(next) =>
                // Name is the only sortable agent column, so direction is the
                // whole choice, and it is the server that applies it.
                setAgentSort(next ?? { id: "identity", direction: "asc" })
              }
              visible={agentRows.length}
              hasMore={agentsQuery.hasNextPage}
              onLoadMore={async () => {
                // Returned, not discarded: the table keeps its loading state
                // until this settles. Guarded, so a second scroll to the end
                // cannot start the same page twice.
                if (agentsQuery.isFetchingNextPage) return;
                await agentsQuery.fetchNextPage();
              }}
              onRowClick={openIdentity}
              emptyMessage={rosterMessage("No agents match these filters")}
            />
          )}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * One kind of identity: its own heading, its own sentence about where the rows
 * come from, and its own columns. A person and an agent share almost nothing
 * but a name, so they share no table.
 */
function IdentityGroup({
  heading,
  count,
  note,
  action,
  columns,
  rows,
  visible,
  hasMore,
  onLoadMore,
  sort,
  onSortChange,
  onRowClick,
  emptyMessage,
}: {
  heading: string;
  count: number;
  note: string;
  action?: ReactNode;
  columns: Column<Employee>[];
  rows: Employee[];
  visible: number;
  /** Overrides "more rows exist" for a list whose rest lives on the server. */
  hasMore?: boolean;
  /** May be async: the table holds its loading state until it settles. */
  onLoadMore: () => void | Promise<void>;
  sort: SortDescriptor | null;
  onSortChange: (next: SortDescriptor | null) => void;
  onRowClick: (row: Employee) => void;
  emptyMessage: ReactNode;
}): JSX.Element {
  return (
    <section className="flex min-h-[9rem] min-w-0 flex-col gap-3 overflow-hidden">
      {/* No heading when the page already carries it: one roster per page
          means the page title and this would say the same word. The count is
          not the heading, so it is still shown on its own. */}
      <div className="flex items-baseline justify-between gap-6">
        <div className="flex min-w-0 items-baseline gap-3">
          {heading && <h2 className="text-base font-medium">{heading}</h2>}
          <span className="text-muted-foreground font-mono text-xs">
            {count} {count === 1 ? "row" : "rows"}
          </span>
          {note && (
            <Text muted small className="truncate">
              {note}
            </Text>
          )}
        </div>
        {action}
      </div>
      {/* The rows scroll inside the pane. More of them load as that scroll
          reaches the end, which is the pagination this list has: neither the
          directory nor the agent inventory takes a cursor yet. */}
      {/* The header stays while its rows move: a column you cannot see is a
          column you cannot read a cell against. */}
      {/* The header is its own table, outside the scroller. Sticky kept it
          in place mid-scroll but it still rode the elastic overscroll at
          either end, because a sticky element is still inside the box that
          bounces. Both tables take their column widths from the same column
          list, so they line up. */}
      {/* Header and rows are one table to the eye: no gap between the two
          elements that make it. */}
      {/* Sideways is one scroll for both tables. A Table is its own
          overflow-x box, so narrow enough to run the fixed-width columns past
          the pane each table scrolled on its own: the rows slid under a header
          that stayed put. The tables give up their own sideways scrolling here
          and this box does it for both at once, and min-w-min sets one width
          they both fill, so a column is the same column in each. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-x-auto overscroll-x-contain [&_table]:overflow-visible">
        <div className="flex min-h-0 min-w-min flex-1 flex-col">
          {/* The sortable header the reader sees. It is the one that carries
              the controls, so the copy below it is hidden from them. */}
          <div className="[&_table]:border-b-0 [&_tbody]:hidden">
            <Table
              columns={columns}
              data={[]}
              rowKey={() => "header"}
              sort={sort}
              onSortChange={onSortChange}
              noResultsMessage={null}
            />
          </div>
          {/* The rows keep a header of their own rather than hideHeader, or a
              screen reader would read this table's cells with no column names
              at all. It is collapsed to nothing instead of removed: the column
              tracks come from the grid template, so a zero-height row costs no
              layout, where sr-only would take the cells out of the subgrid. */}
          <div className="border-border min-h-0 flex-1 overflow-y-auto overscroll-y-contain border-b [&_table]:border-t-0 [&_table]:border-b-0 [&_thead]:h-0 [&_thead]:overflow-hidden [&_thead_th]:h-0 [&_thead_th]:overflow-hidden [&_thead_th]:border-0 [&_thead_th]:p-0">
            <Table
              columns={columns}
              data={rows.slice(0, visible)}
              hasMore={hasMore ?? visible < rows.length}
              onLoadMore={async () => {
                await onLoadMore();
              }}
              rowKey={(row) => row.id}
              onRowClick={onRowClick}
              noResultsMessage={emptyMessage}
            />
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * When the directory knows which account produced the most recent activity,
 * the timestamp names it — the workspace this identity was last working in.
 * Plain text otherwise.
 */
function LastActivityCell({ identity }: { identity: Employee }): JSX.Element {
  if (!identity.mostRecentAccount) {
    return (
      <Text muted small className="truncate">
        {identity.lastActivity}
      </Text>
    );
  }

  return (
    <AccountsPopover
      label={identity.lastActivity}
      title="Most recent account"
      accounts={[identity.mostRecentAccount]}
    />
  );
}

/**
 * The linked accounts behind one row: a count that opens the list, because the
 * addresses themselves are too long to sit in a column and the question a
 * reader has here is "how many, and are any personal".
 */
function AccountsCell({ identity }: { identity: Employee }): JSX.Element {
  const { accounts } = identity;
  if (accounts.length === 0) {
    return <span className="text-muted-foreground/50 text-sm">&mdash;</span>;
  }

  return (
    <AccountsPopover
      label={`${accounts.length} account${accounts.length === 1 ? "" : "s"}`}
      title="Linked accounts"
      accounts={accounts}
    />
  );
}

/**
 * The popover shell both account cells use: a trigger that says how many, and
 * a list naming each one with its provider and team/personal label.
 */
function AccountsPopover({
  label,
  title,
  accounts,
}: {
  label: string;
  title: string;
  accounts: EmployeeAccount[];
}): JSX.Element {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          // The row navigates on click; opening the popover is not that.
          onClick={(event) => event.stopPropagation()}
          className="hover:bg-muted/60 -mx-1.5 flex items-center gap-1.5 px-1.5 py-1 transition-colors"
        >
          {/* text-sm: the same size the cells that do not open a popover use. */}
          <span className="text-muted-foreground text-sm">{label}</span>
          <Icon
            name="chevron-down"
            className="text-muted-foreground/60 size-3"
          />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <div className="border-b px-3 py-2">
          <p className="text-xs font-medium">{title}</p>
        </div>
        <ul className="divide-border/60 max-h-64 divide-y overflow-y-auto">
          {accounts.map((account, index) => (
            <li
              key={`${account.provider}:${account.email}:${index}`}
              className="px-3 py-2"
            >
              <AccountRow account={account} />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

/**
 * Initials for a person whose only name is an address: getInitials splits on
 * spaces, which yields a single letter for `ana.vidal@…`. Read the local part
 * instead so an address-only person still gets a real monogram.
 */
function personInitials(name: string): string {
  if (!name.includes("@")) return getInitials(name);
  const local = name.slice(0, name.indexOf("@"));
  return getInitials(local.replace(/[._-]+/g, " "));
}

/**
 * The leading cell. Every person reads as a person — photo or initials, name in
 * the same weight — and only a registered agent gets a different face. Whether
 * they hold a linked account is the Accounts column's job, which says it once
 * and quietly.
 */
function IdentityCell({ identity }: { identity: Employee }): JSX.Element {
  const agentId = identity.registeredAgentId;
  // A person with no member row has only their address, which is already the
  // name; repeating it underneath would be noise.
  const secondary = identity.email === identity.name ? "" : identity.email;

  if (agentId) {
    // The shared renderer, so this roster draws the same agent face as every
    // other surface rather than a lucide glyph inside a round person avatar.
    //
    // No authorizer: the Owner column already names the person behind this
    // agent, and saying it again on every row would be noise — not because
    // there is nobody to name.
    return (
      <ActingPrincipal
        agent={{ id: agentId, name: identity.name }}
        className="font-medium"
      />
    );
  }

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8">
        {identity.photoUrl && (
          <AvatarImage src={identity.photoUrl} alt={identity.name} />
        )}
        <AvatarFallback className="text-[11px] font-medium">
          {personInitials(identity.name)}
        </AvatarFallback>
      </Avatar>
      <div className="flex min-w-0 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          {/* The row click already navigates here, but a handler is not a
              link: no cmd+click, no middle-click, no copy-link, and nothing
              for a screen reader to announce. On a page whose whole job is
              reaching people, the name itself has to be the anchor. */}
          {/* A name can be a long unbroken address, so it wraps anywhere and
              stops at two lines rather than running past the row. */}
          <Text className="line-clamp-2 min-w-0 font-medium wrap-anywhere">
            <IdentityLink
              identifier={{ urn: identityUrnForEmployee(identity) }}
            >
              {identity.name}
            </IdentityLink>
          </Text>
        </div>
        {secondary && (
          <Text muted small className="truncate text-xs">
            {secondary}
          </Text>
        )}
      </div>
    </div>
  );
}
