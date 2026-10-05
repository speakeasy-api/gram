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
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { useRBAC } from "@/hooks/useRBAC";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { Badge } from "@/components/ui/Badge";
import { Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { dateTimeFormatters } from "@/lib/dates";
import { useHideInsightsDock } from "@/components/insights-context";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { IdentityLink } from "@/components/identity-link";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useQuery } from "@tanstack/react-query";
import { Bot, Plus, User } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Link,
  Navigate,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
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
  const routes = useRoutes();
  const location = useLocation();
  const { identityUrn = "" } = useParams<{ identityUrn: string }>();
  return (
    <Navigate
      to={`${routes.identities.detail.overview.href(
        encodeIdentityUrn(identityUrn),
      )}${location.search}`}
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
  {
    id: "enrollment",
    label: "Enrollment",
    kind: "select",
    allLabel: "All",
    description: "Whether the platform has seen this identity work at all.",
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

const ENROLLMENT_OPTIONS = [
  { value: "enrolled", label: "Enrolled" },
  { value: "not_enrolled", label: "Not enrolled" },
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
        Humans
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
    width: "1.6fr",
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
    width: "1fr",
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
    width: "1fr",
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
    width: "200px",
    sortable: true,
    sortValue: (identity) => identity.lastActivityTimestamp ?? 0,
    render: (identity) => <LastActivityCell identity={identity} />,
  },
  {
    key: "tokens",
    header: "Tokens",
    width: "120px",
    sortable: true,
    sortValue: (identity) => identity.tokenCount,
    render: (identity) => (
      <Text small className="tabular-nums">
        {identity.tokenCount.toLocaleString()}
      </Text>
    ),
  },
];

export default function IdentitiesIndex(): JSX.Element {
  return (
    <RequireScope scope={["project:read"]} level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <IdentitiesIndexContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function IdentitiesIndexContent(): JSX.Element {
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
  const agentManagementFlag = useFeatureFlag(FEATURE_FLAGS.agentManagement);
  const { hasScope } = useRBAC();
  const canReadOrganization = hasScope("org:read", organization.id);
  // Match the existing agent screen's supported sessions, without changing scope.
  const agentsEnabled =
    agentManagementFlag.status === "enabled" &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) === null;
  const agentsQuery = useQuery({
    queryKey: [
      "identities",
      "registered-agents",
      organization.id,
      session.user.id,
    ],
    queryFn: ({ signal }) => fetchRegisteredAgents(sdk, signal),
    enabled: agentsEnabled,
    throwOnError: false,
    retry: false,
  });
  const [search, setSearch] = useState("");
  // Honours `?sort=<column>:<asc|desc>` so a handoff can open the list on the
  // order it was talking about — the dashboard's Top Users "View all" means
  // "these people, by tokens", and landing on last-activity order shows a
  // different set entirely.
  const [sort, setSort] = useState<SortDescriptor | null>(
    parseSortParam(location.search) ?? {
      id: "lastActivity",
      direction: "desc",
    },
  );
  const { values, setValue, clearValue, clearAll } =
    useFilterState(IDENTITY_FILTERS);

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
      ...(agentsEnabled ? (agentsQuery.data ?? []) : []).map(
        registeredAgentIdentity,
      ),
    ],
    [
      membersQuery.data,
      rolesQuery.data,
      usageQuery.data,
      agentsEnabled,
      agentsQuery.data,
    ],
  );

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
  const filterSchema = useMemo(
    () =>
      IDENTITY_FILTERS.filter((dimension) => {
        if (dimension.id === "kind") return hasLegacyKindFilter;
        const selected = (values[dimension.id as keyof typeof values] ??
          []) as string[];
        if (selected.length > 0) return true;
        if (dimension.id === "role") return roleOptions.length > 0;
        if (dimension.id === "department") return departmentOptions.length > 0;
        if (dimension.id === "team") return teamOptions.length > 0;
        if (dimension.id === "device_status") {
          return deviceStatusOptions.length > 0;
        }
        return true;
      }),
    [
      values,
      hasLegacyKindFilter,
      roleOptions.length,
      departmentOptions.length,
      teamOptions.length,
      deviceStatusOptions.length,
    ],
  );

  const accountType = (values.account_type as string | undefined) ?? "";
  const personalAccount = (values.personal_account as string | undefined) ?? "";
  const enrollment = (values.enrollment as string | undefined) ?? "";
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
      if (
        selectedKinds.length > 0 &&
        !selectedKinds.includes(identityKindOf(identity))
      ) {
        return false;
      }
      if (!matchesIdentityTelemetryFilters(identity, enrollment, activity))
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
    identities,
    search,
    kindKey,
    accountType,
    personalAccount,
    enrollment,
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
    () => new Map((agentsQuery.data ?? []).map((agent) => [agent.id, agent])),
    [agentsQuery.data],
  );
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
        render: (identity) => (
          <Text muted small className="truncate">
            {agentsById.get(identity.registeredAgentId ?? "")?.ownerProfile
              ?.displayName ?? "—"}
          </Text>
        ),
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
    ],
    [agentsById],
  );
  // One sorted list, split by what the row is. The filters above still narrow
  // both; the Kind column is dropped inside each table because the heading
  // already says it.
  const peopleRows = useMemo(
    () => sortedRows.filter((row) => identityKindOf(row) !== "agent"),
    [sortedRows],
  );
  // The agents table sorts on its own columns, by its own order: the people
  // table's "last activity, descending" names a column agents do not have.
  const [agentSort, setAgentSort] = useState<SortDescriptor | null>({
    id: "identity",
    direction: "asc",
  });
  const agentRows = useMemo(
    () =>
      sortTableData(
        sortedRows.filter((row) => identityKindOf(row) === "agent"),
        agentColumns,
        agentSort,
      ) as Employee[],
    [sortedRows, agentColumns, agentSort],
  );
  const [peopleVisible, setPeopleVisible] = useState(PAGE_SIZE);
  const [agentsVisible, setAgentsVisible] = useState(PAGE_SIZE);
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
    setAgentsVisible(PAGE_SIZE);
  }, [
    search,
    kindKey,
    accountType,
    personalAccount,
    enrollment,
    activity,
    deviceStatusKey,
    roleKey,
    departmentKey,
    teamKey,
    sort,
  ]);

  return (
    <Page.Section>
      <Page.Section.Title>Identities</Page.Section.Title>
      <Page.Section.Description>
        {/* What the page is for, not what it currently holds: each table
            states its own count. */}
        Who can act through Gram — the people from your identity provider and
        the agents you register — with what each of them reached.
      </Page.Section.Description>
      {/* No page-level action: registering an agent belongs to the agents
          table, which is the only half of this page it applies to. */}
      <Page.Section.Body>
        {/* The section stacks its body children at 8px, which reads as one
            block: the tiles, the controls and the table are three things. */}
        {/* Both tables on screen at once: the page does not scroll past one
            to reach the other, so each pane takes half the room left under the
            controls and scrolls its own rows. */}
        <div className="-mb-24 flex h-[calc(100dvh-21rem)] min-h-[20rem] flex-col gap-6">
          {/* No stat tiles: each table states its own count, and the four
              numbers above them repeated it without saying anything the rows
              do not. */}
          <Page.Toolbar>
            <Page.Toolbar.Leading>
              <SegmentedControl
                value={kindKey}
                onChange={(kind) =>
                  setValue("kind", kind ? kind.split(",") : [])
                }
                options={kindOptions}
              />
            </Page.Toolbar.Leading>
            <Page.Toolbar.Search
              value={search}
              onChange={setSearch}
              placeholder="Search identities…"
              debounceMs={200}
            />
            <Page.Toolbar.Actions>
              <Page.Toolbar.Filters
                schema={filterSchema}
                values={values}
                optionsById={{
                  kind: Object.entries(IDENTITY_KIND_LABELS).map(
                    ([value, label]) => ({ value, label }),
                  ),
                  enrollment: ENROLLMENT_OPTIONS,
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
          {/* Two tables, because the two kinds are not the same kind of
              thing: a person arrives from the identity provider and cannot be
              created here, while an agent is registered here and issued its
              key here. One table hid that, whatever the Kind column said. */}
          {kindKey !== "agent" && (
            <IdentityGroup
              heading="People"
              count={peopleRows.length}
              note="From your identity provider. Gram does not create them."
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
          {kindKey !== "person" && (
            <IdentityGroup
              heading="Agents"
              count={agentRows.length}
              note="Registered here, and issued their keys here."
              action={
                agentsEnabled ? (
                  <Button asChild variant="secondary" size="sm">
                    <Link to={`${routes.agents.href()}?create=true`}>
                      <Plus className="size-4" aria-hidden="true" />
                      New agent identity
                    </Link>
                  </Button>
                ) : undefined
              }
              columns={agentColumns}
              rows={agentRows}
              sort={agentSort}
              onSortChange={setAgentSort}
              visible={agentsVisible}
              onLoadMore={() => setAgentsVisible((count) => count + PAGE_SIZE)}
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
  onLoadMore: () => void;
  sort: SortDescriptor | null;
  onSortChange: (next: SortDescriptor | null) => void;
  onRowClick: (row: Employee) => void;
  emptyMessage: ReactNode;
}): JSX.Element {
  // Equal halves: both tables are on screen whatever either one holds, and
  // each scrolls its own rows rather than pushing the other down.
  return (
    <section className="flex min-h-[9rem] flex-1 basis-0 flex-col gap-3">
      <div className="flex items-baseline justify-between gap-6">
        {/* Title, count and provenance on one line: the note is a caption for
            the heading, not a paragraph under it. */}
        <div className="flex min-w-0 items-baseline gap-3">
          <h2 className="text-base font-medium">{heading}</h2>
          <span className="text-muted-foreground font-mono text-xs">
            {count}
          </span>
          <Text muted small className="truncate">
            {note}
          </Text>
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
      <div className="flex min-h-0 flex-1 flex-col">
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
        <div className="border-border min-h-0 flex-1 overflow-auto overscroll-contain border-b [&_table]:border-t-0 [&_table]:border-b-0">
          <Table
            columns={columns}
            data={rows.slice(0, visible)}
            hideHeader
            hasMore={visible < rows.length}
            onLoadMore={async () => onLoadMore()}
            rowKey={(row) => row.id}
            onRowClick={onRowClick}
            noResultsMessage={emptyMessage}
          />
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
  const isAgent = identityKindOf(identity) === "agent";
  // A person with no member row has only their address, which is already the
  // name; repeating it underneath would be noise.
  const secondary = identity.email === identity.name ? "" : identity.email;

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8">
        {identity.photoUrl && (
          <AvatarImage src={identity.photoUrl} alt={identity.name} />
        )}
        <AvatarFallback className="text-[11px] font-medium">
          {isAgent ? <Bot className="size-4" /> : personInitials(identity.name)}
        </AvatarFallback>
      </Avatar>
      <div className="flex min-w-0 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          {/* The row click already navigates here, but a handler is not a
              link: no cmd+click, no middle-click, no copy-link, and nothing
              for a screen reader to announce. On a page whose whole job is
              reaching people, the name itself has to be the anchor. */}
          {/* An agent's name may be a long unbroken id, so it wraps anywhere
              and stops at two lines rather than running past the row. */}
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
