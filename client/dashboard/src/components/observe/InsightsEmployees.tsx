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
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { IdentityCell } from "@/components/ui/identity-cell";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
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
import { unwrapAsync } from "@gram/client/types/fp";
import { type DateRangePreset, getPresetRange } from "@gram-ai/elements";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Info,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { slugify } from "@/lib/constants";
import {
  Alert,
  Badge,
  Button,
  type Column,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@/components/ui/moonshine";
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
                <Heading variant="h1">Employee Enrollment</Heading>
                <ReleaseStageBadge stage="preview" />
              </div>
              <Type muted small>
                Track platform adoption for organization members in this project
                over {rangeLabel}. Employees with tool or agent session activity
                are marked enrolled; employees without any activity are marked
                not enrolled.
              </Type>
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
                }}
                isRefreshing={
                  membersFetching || rolesFetching || usageQuery.isFetching
                }
              />
            </Page.Toolbar>
          </div>

          {error ? (
            <Alert variant="error" dismissible={false}>
              <span className="font-medium">
                Unable to load employee enrollment data
              </span>
              <div>{error.message}</div>
            </Alert>
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
                  subtext={
                    isUnattributedView
                      ? "Not applicable to unknown users"
                      : "No platform activity found"
                  }
                />
                <MetricCard
                  title="Token Count"
                  value={totalTokenCount}
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
}: {
  employees: Employee[];
  search: string;
  onSelectUser: (employee: Employee) => void;
}) {
  const [page, setPage] = useState(0);
  const [sort, setSort] = useState<SortDescriptor | null>(null);
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
          <IdentityCell
            name={item.name}
            subtitle={item.email}
            imageUrl={item.photoUrl ?? undefined}
          />
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
      {
        key: "accounts",
        header: "Accounts",
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
            <Button
              variant="tertiary"
              size="xs"
              aria-label={`View ${item.name}`}
              onClick={(event) => {
                event.stopPropagation();
                onSelectUser(item);
              }}
            >
              <Button.Text>View</Button.Text>
              <Button.RightIcon>
                <ArrowRight />
              </Button.RightIcon>
            </Button>
          </div>
        ),
      },
    ],
    [onSelectUser],
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
        <Type muted small>
          {search
            ? `No employees matching "${search}".`
            : "No organization members found."}
        </Type>
      </div>
    );
  };

  return (
    <section className="bg-card flex flex-col gap-4">
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
          <Type muted small>
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sortedEmployees.length)} of{" "}
            {sortedEmployees.length}
          </Type>
          <div className="flex items-center gap-1">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
              aria-label="Previous page"
            >
              <Button.LeftIcon>
                <ChevronLeft className="size-4" />
              </Button.LeftIcon>
            </Button>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
              aria-label="Next page"
            >
              <Button.LeftIcon>
                <ChevronRight className="size-4" />
              </Button.LeftIcon>
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
      <Card className="bg-muted/40 gap-4 md:flex-row md:items-center md:justify-between">
        <div className="max-w-3xl space-y-1">
          <Card.Title>How enrollment works</Card.Title>
          <Type muted small>
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
          </Type>
        </div>
        <Button
          size="sm"
          className="shrink-0 md:self-center"
          onClick={() => setShowSetupDialog(true)}
        >
          Set up hooks
        </Button>
      </Card>
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
          <div key={index} className="bg-card border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="bg-card border p-5">
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
        <Button
          variant="tertiary"
          size="xs"
          className="-mx-1.5 gap-1.5"
          // Don't let the row's navigate handler fire when opening the popover.
          onClick={(e) => e.stopPropagation()}
        >
          <Button.Text className={cn("text-muted-foreground", labelClassName)}>
            {label}
          </Button.Text>
          <Button.RightIcon>
            <ChevronDown className="text-muted-foreground/60 size-3" />
          </Button.RightIcon>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <div className="border-b px-3 py-2">
          <Type as="p" mono small muted className="uppercase tracking-[0.08em]">
            {title}
          </Type>
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
