import { AccountRow } from "@/components/observe/account-display";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { useInsightsState } from "@/components/insights-context";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { ErrorAlert } from "@/components/ui/alert";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/skeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import {
  buildEmployees,
  type Employee,
  type EmployeeAccount,
  type EmployeeStatus,
  isUnattributedEmployee,
} from "@/components/observe/insightsEmployeesData";
import { ACCOUNT_TYPE_OPTIONS } from "@/components/observe/observeFilterConstants";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useSyncedAgentUsers } from "@gram/client/react-query/syncedAgentUsers.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type DateRangePreset, getPresetRange } from "@/elements";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Info } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { dateTimeFormatters } from "@/lib/dates";
import { slugify } from "@/lib/constants";
import {
  Badge,
  type Column,
  Icon,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@speakeasy-api/moonshine";
import { HooksSetupDialog } from "@/pages/hooks/HooksSetupDialog";

type EmployeeView = "employees" | "unattributed";

const VIEW_SEARCH_PARAM = "view";

const EMPLOYEE_VIEWS: EmployeeView[] = ["employees", "unattributed"];
const VIEW_LABELS: Record<EmployeeView, string> = {
  employees: "Employees",
  unattributed: "Unknown users",
};
const VIEW_TOOLTIPS: Record<EmployeeView, string> = {
  employees: "Activity attributed to known organization members",
  unattributed: "Activity that couldn't be matched to a member",
};

// Unified filter schema for the Employees page. The date range is pinned (and
// now URL-persisted, replacing the previous local state). Status, role and user
// map onto the old "filter dimension" dropdown: role/user are independent
// selects that are ANDed together, and status filters on enrollment.
const EMPLOYEE_FILTERS = defineFilters([
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: "30d",
  },
  { id: "status", label: "Enrollment status", kind: "multiselect" },
  {
    id: "account_type",
    label: "Account type",
    kind: "select",
    allLabel: "All",
  },
  { id: "role", label: "Role", kind: "select" },
  { id: "user", label: "User", kind: "select" },
]);

const STATUS_OPTIONS = [
  { value: "enrolled", label: "Enrolled" },
  { value: "not_enrolled", label: "Not enrolled" },
];

const PRESET_RANGE_LABELS: Record<DateRangePreset, string> = {
  "15m": "the last 15 minutes",
  "1h": "the last hour",
  "4h": "the last 4 hours",
  "1d": "the last day",
  "2d": "the last 2 days",
  "3d": "the last 3 days",
  "7d": "the last 7 days",
  "15d": "the last 15 days",
  "30d": "the last 30 days",
  "90d": "the last 90 days",
};

function presetRangeLabel(preset: DateRangePreset): string {
  return PRESET_RANGE_LABELS[preset] ?? "the selected range";
}

// Right-aligned Employees vs Unknown-users toggle, rendered in the toolbar's
// Actions slot. Reads/writes the `?view` URL param exactly as before.
function EmployeeViewToggle({
  view,
  onViewChange,
  disabled,
}: {
  view: EmployeeView;
  onViewChange: (view: EmployeeView) => void;
  disabled?: boolean;
}) {
  return (
    <SegmentedControl
      value={view}
      onChange={onViewChange}
      disabled={disabled}
      options={EMPLOYEE_VIEWS.map((value) => ({
        value,
        label: VIEW_LABELS[value],
        tooltip: VIEW_TOOLTIPS[value],
      }))}
    />
  );
}

const statusMeta: Record<
  EmployeeStatus,
  { label: string; variant: "success" | "destructive" }
> = {
  enrolled: {
    label: "Enrolled",
    variant: "success",
  },
  not_enrolled: {
    label: "Not Enrolled",
    variant: "destructive",
  },
};

// Device-agent activity is a separate signal from enrollment: it's whether the
// Speakeasy device agent (org-scoped) has polled recently for this member's
// email, not whether their AI tools report telemetry. Surfaced here as an
// optional column so admins can see enrollment + agent health in one table.
const AGENT_ACTIVE_WINDOW_MS = 5 * 60 * 1000;
const AGENT_STATUS_TICK_MS = 30 * 1000;

type DeviceAgentState = "active" | "stale" | "none";

// Whole-column state: "hidden" when the feature is off; otherwise it tracks the
// listSyncedUsers query so cells can show loading/error instead of a misleading
// "Not Enrolled" derived from a not-yet-populated map.
type DeviceAgentColumnStatus = "hidden" | "loading" | "error" | "ready";

// Rank for sorting: active devices first, then stale, then members with no
// agent at all — so the rows an admin cares about surface at the top.
const AGENT_SORT_RANK: Record<DeviceAgentState, number> = {
  active: 0,
  stale: 1,
  none: 2,
};

function deviceAgentState(
  lastSeen: Date | undefined,
  now: number,
): DeviceAgentState {
  if (!lastSeen) return "none";
  return now - lastSeen.getTime() < AGENT_ACTIVE_WINDOW_MS ? "active" : "stale";
}

// useNow returns a wall-clock timestamp that advances every intervalMs so
// time-derived values (here, device Active/Stale) recompute on their own on a
// long-open dashboard. Pass intervalMs <= 0 to disable ticking (when the derived
// value isn't shown).
function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (intervalMs <= 0) return;
    // Refresh immediately on (re-)enable: the tick may turn on well after mount
    // (e.g. once a slow listSyncedUsers query becomes "ready"), and without this
    // the first render would compute statuses against the mount-time timestamp
    // until the first interval fires, delaying near-threshold Active→Stale flips.
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function InsightsEmployeesContent(): JSX.Element {
  const client = useGramContext();
  const routes = useRoutes();
  const { projectSlug } = useSlugs();
  const navigate = useNavigate();
  const { isExpanded: isInsightsOpen } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
    refetch: refetchMembers,
    isFetching: membersFetching,
  } = useMembers();
  const {
    data: rolesData,
    isLoading: rolesLoading,
    refetch: refetchRoles,
    isFetching: rolesFetching,
  } = useRoles();
  // Device agent status is a preview, org-level feature. Only fetch + show the
  // column when the org has it enabled; the endpoint is org-admin gated, which
  // the Observe pages already require.
  const isDeviceAgentEnabled =
    useTelemetry().isFeatureEnabled("gram-device-agent") ?? false;
  const {
    data: deviceSyncData,
    isError: deviceSyncsError,
    refetch: refetchDeviceSyncs,
    isFetching: deviceSyncsFetching,
  } = useSyncedAgentUsers(undefined, undefined, {
    enabled: isDeviceAgentEnabled,
  });
  // Drive the column off the query's own state, not just its data: an empty map
  // while loading or after an error must not read as "everyone Not Enrolled".
  // "ready" only once data has actually arrived.
  const deviceStatus: DeviceAgentColumnStatus = !isDeviceAgentEnabled
    ? "hidden"
    : deviceSyncsError
      ? "error"
      : deviceSyncData === undefined
        ? "loading"
        : "ready";

  const { values, setValue, clearValue, clearAll } =
    useFilterState(EMPLOYEE_FILTERS);
  const [searchParams, setSearchParams] = useSearchParams();
  const view: EmployeeView =
    searchParams.get(VIEW_SEARCH_PARAM) === "unattributed"
      ? "unattributed"
      : "employees";
  const isUnattributedView = view === "unattributed";

  const selectedStatuses = values.status;
  const selectedRoleId = values.role;
  // Unknown users carry a placeholder role, so the role filter doesn't apply in
  // that view. Derived (rather than cleared in the view-change handler) so it
  // also holds when the view changes through the URL, e.g. back/forward nav.
  const effectiveRoleId = isUnattributedView ? null : selectedRoleId;
  const selectedUserId = values.user;
  const selectedAccountType = values.account_type;

  const handleViewChange = useCallback(
    (next: EmployeeView) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (next === "employees") {
            params.delete(VIEW_SEARCH_PARAM);
          } else {
            params.set(VIEW_SEARCH_PARAM, next);
          }
          // The selected user may not exist in the other view.
          params.delete("user");
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Bridge the unified date range back to the from/to the usage query consumes.
  const { from, to } = useMemo(() => {
    const d = values.date;
    if (d.customRange) return d.customRange;
    return getPresetRange(d.preset ?? "30d");
  }, [values.date]);
  const rangeLabel = useMemo(() => {
    const d = values.date;
    if (d.customRange) return d.customLabel ?? "the selected range";
    return presetRangeLabel(d.preset ?? "30d");
  }, [values.date]);

  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData]);
  // email -> most recent device-agent sync, used to derive the per-row status.
  const deviceSyncByEmail = useMemo(() => {
    const map = new Map<string, Date>();
    for (const u of deviceSyncData?.users ?? []) {
      map.set(u.email.toLowerCase(), u.lastSeenAt);
    }
    return map;
  }, [deviceSyncData]);
  const usageQuery = useQuery({
    queryKey: [
      "insights",
      "employees",
      "usage",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchEmployeeUsage(client, from, to),
    throwOnError: false,
  });
  const usageSummaries = useMemo(
    () => usageQuery.data ?? [],
    [usageQuery.data],
  );
  const isLoading = membersLoading || rolesLoading || usageQuery.isLoading;
  const error = membersError ?? usageQuery.error;
  const allEmployees = useMemo(
    () => buildEmployees(members, roles, usageSummaries),
    [members, roles, usageSummaries],
  );
  const roleNameById = useMemo(
    () => new Map(roles.map((role) => [role.id, role.name])),
    [roles],
  );
  const viewEmployees = useMemo(
    () =>
      allEmployees.filter((item) =>
        isUnattributedView
          ? isUnattributedEmployee(item)
          : !isUnattributedEmployee(item),
      ),
    [allEmployees, isUnattributedView],
  );
  // Apply the unified status/role/user filters to the current view's employees.
  // role/user are independent selects that are ANDed together; status filters on
  // enrollment. role is skipped in the unattributed view (placeholder roles).
  const employees = useMemo(() => {
    const roleName = effectiveRoleId
      ? roleNameById.get(effectiveRoleId)
      : undefined;
    return viewEmployees.filter((item) => {
      if (
        selectedStatuses.length > 0 &&
        !selectedStatuses.includes(item.status)
      )
        return false;
      if (selectedUserId && item.id !== selectedUserId) return false;
      if (effectiveRoleId && item.role !== roleName) return false;
      // Each filter matches employees holding at least one account of that type;
      // an employee with both a team and a personal account shows under either.
      if (selectedAccountType === "personal" && !item.hasPersonalAccount)
        return false;
      if (
        selectedAccountType === "team" &&
        !item.accounts.some((a) => a.accountType === "team")
      )
        return false;
      return true;
    });
  }, [
    viewEmployees,
    selectedStatuses,
    selectedUserId,
    effectiveRoleId,
    selectedAccountType,
    roleNameById,
  ]);

  // Schema for the bar: role is sheet-only in the unattributed view (its options
  // don't apply there), so it's dropped from the rendered schema then.
  const filterSchema = useMemo(
    () =>
      isUnattributedView
        ? EMPLOYEE_FILTERS.filter((d) => d.id !== "role")
        : EMPLOYEE_FILTERS,
    [isUnattributedView],
  );

  // Page-supplied option lists. User options reflect the current view; role
  // options derive from the org roles (value = role id, matching the old logic).
  const optionsById = useMemo<OptionsById>(
    () => ({
      status: STATUS_OPTIONS,
      account_type: ACCOUNT_TYPE_OPTIONS,
      role: roles.map((role) => ({ value: role.id, label: role.name })),
      user: viewEmployees.map((item) => ({ value: item.id, label: item.name })),
    }),
    [roles, viewEmployees],
  );

  const totalEmployees = employees.length;
  const enrolledEmployees = employees.filter(
    (item) => item.status === "enrolled",
  ).length;
  const notEnrolledEmployees = totalEmployees - enrolledEmployees;
  const totalTokenCount = employees.reduce(
    (sum, item) => sum + item.tokenCount,
    0,
  );
  const employeesBase = routes.employees.href();
  const openUser = (employee: Employee) => {
    void navigate(`${employeesBase}/${routeSegmentForEmployee(employee)}`);
  };
  const enrollmentRate =
    totalEmployees > 0 ? (enrolledEmployees / totalEmployees) * 100 : 0;

  // Per-table name/email search, lifted up so it can live in the toolbar's
  // Search slot. `page` is owned by the table; resetting it on search happens
  // there via the `search` prop changing.
  const [search, setSearch] = useState("");

  // Reset every unified filter (clearAll already does a single setSearchParams)
  // AND the `?view` param in one shot — firing multiple synchronous
  // setSearchParams would have react-router clobber all but the last.
  const handleClearAll = useCallback(() => {
    setSearch("");
    clearAll();
  }, [clearAll]);

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="What would you like to know about employee enrollment?"
        subtitle="Ask who is enrolled, who still needs setup, and how platform adoption is tracking across the team"
        contextInfo={`Project-scoped Employees tab: ${enrolledEmployees} of ${totalEmployees} employees have hooks activity in ${rangeLabel} and are enrolled; ${notEnrolledEmployees} employees have no hooks activity and are not enrolled.`}
        suggestions={INSIGHTS_SUGGESTIONS["insights/employees"]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <div className="flex flex-col gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex items-center gap-2">
                <h1 className="text-xl font-semibold">Employee Enrollment</h1>
                <ReleaseStageBadge stage="preview" />
              </div>
              <p className="text-muted-foreground text-sm">
                Track platform adoption for organization members in this project
                over {rangeLabel}. Employees with tool or agent session activity
                are marked enrolled; employees without any activity are marked
                not enrolled.
              </p>
            </div>
            <Page.Toolbar>
              <Page.Toolbar.Search
                value={search}
                onChange={setSearch}
                placeholder="Search by name or email..."
              />
              <Page.Toolbar.Filters
                schema={filterSchema}
                values={values}
                optionsById={optionsById}
                onChange={setValue as (id: string, value: FilterValue) => void}
                onClear={clearValue as (id: string) => void}
                onClearAll={handleClearAll}
                projectSlug={projectSlug}
              />
              <Page.Toolbar.Actions>
                <EmployeeViewToggle
                  view={view}
                  onViewChange={handleViewChange}
                  disabled={isLoading}
                />
              </Page.Toolbar.Actions>
              <Page.Toolbar.Refresh
                onRefresh={() => {
                  void refetchMembers();
                  void refetchRoles();
                  void usageQuery.refetch();
                  if (isDeviceAgentEnabled) void refetchDeviceSyncs();
                }}
                isRefreshing={
                  membersFetching ||
                  rolesFetching ||
                  usageQuery.isFetching ||
                  deviceSyncsFetching
                }
              />
            </Page.Toolbar>
          </div>

          {error ? (
            <ErrorAlert
              title="Unable to load employee enrollment data"
              error={error}
            />
          ) : isLoading ? (
            <EmployeesLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
                )}
              >
                <MetricCard
                  title={isUnattributedView ? "Unknown users" : "Employees"}
                  value={totalEmployees}
                  icon="user"
                  accentColor="blue"
                  subtext={
                    isUnattributedView
                      ? "Usage not matched to a member"
                      : "Organization members"
                  }
                />
                <MetricCard
                  title="Enrolled"
                  value={enrolledEmployees}
                  displayValue={isUnattributedView ? "-" : undefined}
                  icon="circle-check"
                  accentColor="green"
                  subtext={
                    isUnattributedView
                      ? "Not applicable to unknown users"
                      : "Platform activity present"
                  }
                />
                <MetricCard
                  title="Not Enrolled"
                  value={notEnrolledEmployees}
                  displayValue={isUnattributedView ? "-" : undefined}
                  icon="triangle-alert"
                  accentColor="orange"
                  subtext={
                    isUnattributedView
                      ? "Not applicable to unknown users"
                      : "No platform activity found"
                  }
                />
                <MetricCard
                  title="Token Count"
                  value={totalTokenCount}
                  icon="gauge"
                  accentColor="purple"
                  subtext={
                    isUnattributedView
                      ? undefined
                      : `${enrollmentRate.toFixed(0)}% enrolled`
                  }
                />
              </section>

              <EmployeeTable
                key={view}
                employees={employees}
                search={search}
                onSelectUser={openUser}
                deviceSyncByEmail={deviceSyncByEmail}
                deviceStatus={deviceStatus}
              />
              <EnrollmentLegend />
            </>
          )}
        </div>
      </div>
    </>
  );
}

const PAGE_SIZE = 10;

function EmployeeTable({
  employees,
  search,
  onSelectUser,
  deviceSyncByEmail,
  deviceStatus,
}: {
  employees: Employee[];
  search: string;
  onSelectUser: (employee: Employee) => void;
  deviceSyncByEmail: Map<string, Date>;
  deviceStatus: DeviceAgentColumnStatus;
}) {
  const showDeviceAgent = deviceStatus !== "hidden";
  const [page, setPage] = useState(0);
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  // Only ticks once statuses are actually resolvable; disabled (0) otherwise so
  // the memo stays stable (deviceAgentState is only called when "ready").
  const now = useNow(deviceStatus === "ready" ? AGENT_STATUS_TICK_MS : 0);
  const filteredEmployees = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return employees;
    return employees.filter(
      (item) =>
        item.name.toLowerCase().includes(query) ||
        item.email.toLowerCase().includes(query),
    );
  }, [employees, search]);
  const columns = useMemo<Column<Employee>[]>(
    () => [
      {
        key: "name",
        id: "employee",
        header: (
          <span className="flex items-center gap-1">
            Employee
            <SimpleTooltip tooltip="Enrollment is inferred by matching the email reported by each AI coding tool to the member's platform account. Members without a platform account won't appear as enrolled until they sign up or directory sync is configured.">
              <Info className="text-muted-foreground size-3 shrink-0" />
            </SimpleTooltip>
          </span>
        ),
        sortable: true,
        sortLabel: "Employee",
        sortValue: (item) => item.name.toLowerCase(),
        width: "2fr",
        render: (item) => (
          <div className="flex items-center gap-3">
            <Avatar className="size-9">
              {item.photoUrl && (
                <AvatarImage src={item.photoUrl} alt={item.name} />
              )}
              <AvatarFallback className="text-sm font-semibold">
                {getInitials(item.name)}
              </AvatarFallback>
            </Avatar>
            <div>
              <p className="font-medium">{item.name}</p>
              <p className="text-muted-foreground text-xs">{item.email}</p>
            </div>
          </div>
        ),
      },
      {
        key: "role",
        header: "Role",
        sortable: true,
        sortValue: (item) => item.role.toLowerCase(),
        width: "1fr",
        render: (item) => <span>{item.role}</span>,
      },
      {
        key: "status",
        header: "Enrollment",
        sortable: true,
        sortLabel: "Enrollment",
        sortValue: (item) => statusMeta[item.status].label,
        width: "1fr",
        render: (item) => <StatusPill status={item.status} />,
      },
      ...(showDeviceAgent
        ? [
            {
              key: "deviceAgent",
              header: (
                <span className="flex items-center gap-1">
                  Device Agent
                  <SimpleTooltip tooltip="Whether the Speakeasy device agent on this member's machine has checked in recently. Active = synced in the last few minutes; Stale = enrolled but not seen recently; Not Enrolled = no agent activity.">
                    <Info className="text-muted-foreground size-3 shrink-0" />
                  </SimpleTooltip>
                </span>
              ),
              // Only meaningfully sortable once statuses have resolved; while
              // loading/errored every row ranks equal, preserving base order.
              sortable: true,
              sortLabel: "Device Agent",
              sortValue: (item) =>
                deviceStatus === "ready"
                  ? AGENT_SORT_RANK[
                      deviceAgentState(
                        deviceSyncByEmail.get(item.email.toLowerCase()),
                        now,
                      )
                    ]
                  : 0,
              width: "1fr",
              render: (item) => (
                <DeviceAgentCell
                  status={deviceStatus}
                  lastSeen={deviceSyncByEmail.get(item.email.toLowerCase())}
                  now={now}
                />
              ),
            } satisfies Column<Employee>,
          ]
        : []),
      {
        key: "accounts",
        header: (
          <span className="flex items-center gap-1">
            Accounts
            <SimpleTooltip tooltip="The AI provider accounts (Claude, Codex, Cursor) each employee has been seen using, labelled team or personal. Accounts are linked automatically from tool activity, so this stays blank until an employee is seen using a recognized account.">
              <Info className="text-muted-foreground size-3 shrink-0" />
            </SimpleTooltip>
          </span>
        ),
        sortable: true,
        sortLabel: "Accounts",
        // Personal-holders first (ascending), then more accounts before fewer,
        // so the rows worth a second look group at the top.
        sortValue: (item) =>
          (item.hasPersonalAccount ? 0 : 1_000_000) - item.accounts.length,
        width: "1fr",
        render: (item) => <AccountsCell employee={item} />,
      },
      {
        key: "tokenCount",
        header: "Token Count",
        sortable: true,
        sortValue: (item) => item.tokenCount,
        width: "1fr",
        render: (item) => (
          <span className="font-mono">{item.tokenCount.toLocaleString()}</span>
        ),
      },
      {
        key: "lastActivity",
        header: "Last Activity",
        sortable: true,
        sortValue: (item) => item.lastActivityTimestamp,
        width: "1fr",
        render: (item) => <LastActivityCell employee={item} />,
      },
      {
        key: "action",
        header: "",
        width: "auto",
        render: (item) => (
          <div className="text-right">
            <button
              type="button"
              className="flex items-center gap-1"
              aria-label={`View ${item.name}`}
              onClick={(event) => {
                event.stopPropagation();
                onSelectUser(item);
              }}
            >
              View
              <Icon name="arrow-right" />
            </button>
          </div>
        ),
      },
    ],
    [onSelectUser, showDeviceAgent, deviceStatus, deviceSyncByEmail, now],
  );
  const sortedEmployees = useMemo(() => {
    if (sort?.id === "lastActivity") {
      return filteredEmployees.slice().sort((a, b) => {
        if (
          a.lastActivityTimestamp == null &&
          b.lastActivityTimestamp == null
        ) {
          return 0;
        }
        if (a.lastActivityTimestamp == null) return 1;
        if (b.lastActivityTimestamp == null) return -1;

        const comparison = a.lastActivityTimestamp - b.lastActivityTimestamp;
        return sort.direction === "asc" ? comparison : -comparison;
      });
    }

    return sortTableData(filteredEmployees, columns, sort) as Employee[];
  }, [columns, filteredEmployees, sort]);
  const totalPages = Math.ceil(sortedEmployees.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageEmployees = sortedEmployees.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );
  // Search is owned by the page (toolbar Search slot); jump back to the
  // first page whenever the query changes.
  useEffect(() => {
    setPage(0);
  }, [search]);

  const NoResultsMessage = () => {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-muted-foreground text-sm">
          {search
            ? `No employees matching "${search}".`
            : "No organization members found."}
        </p>
      </div>
    );
  };

  return (
    <section className="bg-card flex flex-col">
      <Table
        columns={columns}
        data={pageEmployees}
        rowKey={(item) => item.id}
        onRowClick={(item) => onSelectUser(item)}
        sort={sort}
        onSortChange={(nextSort) => {
          setSort(nextSort);
          setPage(0);
        }}
        noResultsMessage={<NoResultsMessage />}
      />
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t px-4 py-3">
          <p className="text-muted-foreground text-sm">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sortedEmployees.length)} of{" "}
            {sortedEmployees.length}
          </p>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

function routeSegmentForEmployee(employee: Employee) {
  if (isUnattributedEmployee(employee) && employee.name.includes("@")) {
    return encodeURIComponent(employee.name);
  }
  return slugify(employee.name);
}

function EnrollmentLegend() {
  const [showSetupDialog, setShowSetupDialog] = useState(false);
  const routes = useRoutes();

  return (
    <>
      <section className="bg-muted/40 border-border flex flex-col gap-4 rounded-xl border p-5 md:flex-row md:items-center md:justify-between">
        <div className="max-w-3xl space-y-1">
          <h2 className="text-sm font-semibold">How enrollment works</h2>
          <p className="text-muted-foreground text-sm">
            Employees appear as enrolled once the{" "}
            <Link
              to={routes.plugins.href()}
              className="hover:text-foreground underline underline-offset-2"
            >
              Observability plugin
            </Link>{" "}
            is installed in their AI agent and sends activity to this project.
            Not enrolled yet? Install the observability plugin to start tracking
            their usage.
          </p>
        </div>
        <Button
          size="sm"
          className="shrink-0 md:self-center"
          onClick={() => setShowSetupDialog(true)}
        >
          Set up hooks
        </Button>
      </section>
      <HooksSetupDialog
        open={showSetupDialog}
        onOpenChange={setShowSetupDialog}
      />
    </>
  );
}

function EmployeesLoadingState({
  isInsightsOpen,
}: {
  isInsightsOpen: boolean;
}) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
        )}
      >
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="bg-card rounded-lg border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="bg-card rounded-xl border p-5">
        <Skeleton className="h-5 w-44" />
        <Skeleton className="mt-2 h-4 w-80" />
        <div className="mt-6 space-y-3">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      </section>
    </>
  );
}

function StatusPill({ status }: { status: EmployeeStatus }) {
  const meta = statusMeta[status];

  return (
    <Badge variant={meta.variant}>
      <Badge.Text>{meta.label}</Badge.Text>
    </Badge>
  );
}

// Traffic-light progression: green "Active" (synced recently), amber "Stale"
// (enrolled but not seen lately), grey "Not Enrolled" (no agent activity — a
// distinct, scannable state rather than a blank cell). Grey keeps it visually
// separate from the Enrollment column's red "Not Enrolled".
const deviceAgentMeta: Record<
  DeviceAgentState,
  { label: string; variant: "success" | "warning" | "neutral" }
> = {
  active: { label: "Active", variant: "success" },
  stale: { label: "Stale", variant: "warning" },
  none: { label: "Not Enrolled", variant: "neutral" },
};

// Device-agent status cell. Until the listSyncedUsers query resolves the cell
// shows a skeleton (loading) or a muted "unknown" dash (error) rather than a
// badge — an empty map must never render as a definitive "Not Enrolled". Once
// "ready": an Active/Stale/Not Enrolled badge, with the last sync time on hover.
function DeviceAgentCell({
  status,
  lastSeen,
  now,
}: {
  status: DeviceAgentColumnStatus;
  lastSeen: Date | undefined;
  now: number;
}) {
  if (status === "loading") {
    return <Skeleton className="h-5 w-20 rounded-md" />;
  }
  if (status === "error") {
    return (
      <SimpleTooltip tooltip="Couldn't load device agent status. Try refreshing.">
        <span className="text-muted-foreground/50 text-sm">—</span>
      </SimpleTooltip>
    );
  }
  const meta = deviceAgentMeta[deviceAgentState(lastSeen, now)];
  const tooltip = lastSeen
    ? `Last synced ${dateTimeFormatters.humanize(lastSeen)}`
    : "No device agent activity for this member.";
  return (
    <SimpleTooltip tooltip={tooltip}>
      <Badge variant={meta.variant}>
        <Badge.Text>{meta.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

// Shared popover shell for table cells that reveal linked accounts: a clickable
// trigger (label + chevron) opening a popover that lists each account with its
// email, provider, and type.
function AccountsPopover({
  label,
  labelClassName,
  title,
  accounts,
}: {
  label: string;
  labelClassName?: string;
  title: string;
  accounts: EmployeeAccount[];
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          // Don't let the row's navigate handler fire when opening the popover.
          onClick={(e) => e.stopPropagation()}
          className="hover:bg-muted/60 -mx-1.5 flex items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors"
        >
          <span className={cn("text-muted-foreground", labelClassName)}>
            {label}
          </span>
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
          {accounts.map((a, i) => (
            <li key={`${a.provider}:${a.email}:${i}`} className="px-3 py-2">
              <AccountRow account={a} />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

// Per-employee accounts cell: a clickable trigger summarizing the linked
// accounts (count), opening a popover that lists every account with its email,
// provider, and type. Robust to any number of accounts across providers.
function AccountsCell({ employee }: { employee: Employee }) {
  const { accounts } = employee;
  if (accounts.length === 0) {
    return <span className="text-muted-foreground/50 text-sm">—</span>;
  }

  return (
    <AccountsPopover
      label={`${accounts.length} account${accounts.length === 1 ? "" : "s"}`}
      labelClassName="text-xs"
      title="Linked accounts"
      accounts={accounts}
    />
  );
}

// Last-activity cell: when the directory knows which account produced the most
// recent activity, the timestamp becomes a dropdown identifying that account —
// the workspace the employee was last working in. Plain text otherwise.
function LastActivityCell({ employee }: { employee: Employee }) {
  if (!employee.mostRecentAccount) {
    return (
      <span className="text-muted-foreground">{employee.lastActivity}</span>
    );
  }

  return (
    <AccountsPopover
      label={employee.lastActivity}
      title="Most recent account"
      accounts={[employee.mostRecentAccount]}
    />
  );
}

function getInitials(name: string) {
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

async function fetchEmployeeUsage(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
          },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}
