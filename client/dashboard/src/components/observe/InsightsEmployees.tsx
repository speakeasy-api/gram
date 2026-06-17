import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { useInsightsState } from "@/components/insights-context";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { ErrorAlert } from "@/components/ui/alert";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { SearchBar } from "@/components/ui/search-bar";
import { Skeleton } from "@/components/ui/skeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import {
  buildEmployees,
  type Employee,
  type EmployeeStatus,
  isUnattributedEmployee,
} from "@/components/observe/insightsEmployeesData";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type { UserSummary } from "@gram/client/models/components";
import { useGramContext, useMembers, useRoles } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { type DateRangePreset, getPresetRange } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Info,
} from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
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

type EmployeeFilterDimension = "all" | "user" | "role";
type FilterOption = { id: string; label: string };

type EmployeeView = "employees" | "unattributed";

const VIEW_SEARCH_PARAM = "view";

const EMPLOYEE_VIEWS: EmployeeView[] = ["employees", "unattributed"];
const VIEW_LABELS: Record<EmployeeView, string> = {
  employees: "Employees",
  unattributed: "Unknown users",
};

const EMPLOYEE_FILTER_DIMENSIONS: EmployeeFilterDimension[] = [
  "all",
  "user",
  "role",
];
const EMPLOYEE_FILTER_LABELS: Record<EmployeeFilterDimension, string> = {
  all: "All",
  user: "User",
  role: "Role",
};
const EMPLOYEE_FILTER_PLURAL_LABELS: Record<EmployeeFilterDimension, string> = {
  all: "Items",
  user: "Users",
  role: "Roles",
};

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

function EmployeeFilterBar({
  view,
  onViewChange,
  dimensions,
  dimension,
  onDimensionChange,
  selectedValue,
  onValueChange,
  options,
  disabled,
}: {
  view: EmployeeView;
  onViewChange: (view: EmployeeView) => void;
  dimensions: EmployeeFilterDimension[];
  dimension: EmployeeFilterDimension;
  onDimensionChange: (dimension: EmployeeFilterDimension) => void;
  selectedValue: string | null;
  onValueChange: (value: string | null) => void;
  options: FilterOption[];
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);

  const selectedOption = options.find((o) => o.id === selectedValue);
  const displayLabel =
    dimension === "all"
      ? "All"
      : selectedOption
        ? selectedOption.label || selectedOption.id
        : `All ${EMPLOYEE_FILTER_PLURAL_LABELS[dimension]}`;

  return (
    <div
      className={`flex items-center gap-2 ${disabled ? "pointer-events-none opacity-50" : ""}`}
    >
      <span className="text-muted-foreground hidden text-sm font-medium 2xl:inline">
        Filter by
      </span>
      <div className="border-border flex h-[42px] items-center rounded-md border p-1">
        {EMPLOYEE_VIEWS.map((value) => {
          const isSelected = view === value;
          return (
            <button
              key={value}
              onClick={() => onViewChange(value)}
              disabled={disabled}
              className={`
                h-8 rounded px-3 text-sm font-medium transition-all duration-150
                ${
                  isSelected
                    ? "text-foreground bg-white shadow-sm dark:bg-gray-900"
                    : "text-muted-foreground hover:text-foreground"
                }
                disabled:cursor-not-allowed
              `}
            >
              {VIEW_LABELS[value]}
            </button>
          );
        })}

        <div className="bg-border/50 mx-1 h-6 w-px" />
        {dimensions.map((value) => {
          const isSelected = dimension === value;
          return (
            <button
              key={value}
              onClick={() => onDimensionChange(value)}
              disabled={disabled}
              className={`
                h-8 rounded px-3 text-sm font-medium transition-all duration-150
                ${
                  isSelected
                    ? "text-foreground bg-white shadow-sm dark:bg-gray-900"
                    : "text-muted-foreground hover:text-foreground"
                }
                disabled:cursor-not-allowed
              `}
            >
              {EMPLOYEE_FILTER_LABELS[value]}
            </button>
          );
        })}

        <div className="bg-border/50 mx-1 h-6 w-px" />
        <Popover
          open={dimension !== "all" && !disabled && open}
          onOpenChange={setOpen}
        >
          <PopoverTrigger asChild>
            <button
              disabled={dimension === "all" || disabled}
              className={`flex h-8 min-w-[140px] items-center justify-between gap-2 rounded px-2 text-sm transition-colors ${
                dimension === "all" || disabled
                  ? "cursor-not-allowed opacity-40"
                  : "hover:bg-muted/50"
              }`}
            >
              <span className="max-w-[120px] truncate">{displayLabel}</span>
              <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-[220px] p-0" align="end">
            <Command>
              <CommandInput
                placeholder={`Search ${EMPLOYEE_FILTER_PLURAL_LABELS[dimension].toLowerCase()}...`}
                className="h-9"
              />
              <CommandList>
                <CommandEmpty>No results found.</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    value="__all__"
                    onSelect={() => {
                      onValueChange(null);
                      setOpen(false);
                    }}
                    className="cursor-pointer"
                  >
                    <Check
                      className={`mr-2 size-4 ${selectedValue === null ? "opacity-100" : "opacity-0"}`}
                    />
                    <span>All {EMPLOYEE_FILTER_PLURAL_LABELS[dimension]}</span>
                  </CommandItem>
                  {options.map((option) => (
                    <CommandItem
                      key={option.id}
                      value={option.label || option.id}
                      onSelect={() => {
                        onValueChange(option.id);
                        setOpen(false);
                      }}
                      className="cursor-pointer"
                    >
                      <Check
                        className={`mr-2 size-4 ${selectedValue === option.id ? "opacity-100" : "opacity-0"}`}
                      />
                      <span className="truncate">
                        {option.label || option.id}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
    </div>
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
  } = useMembers();
  const { data: rolesData, isLoading: rolesLoading } = useRoles();

  const [dateRange, setDateRange] = useState<DateRangePreset>("30d");
  const [customRange, setCustomRange] = useState<{
    from: Date;
    to: Date;
  } | null>(null);
  const [customRangeLabel, setCustomRangeLabel] = useState<string | null>(null);
  const [rawFilterDimension, setFilterDimension] =
    useState<EmployeeFilterDimension>("all");
  const [selectedFilterValue, setSelectedFilterValue] = useState<string | null>(
    null,
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const view: EmployeeView =
    searchParams.get(VIEW_SEARCH_PARAM) === "unattributed"
      ? "unattributed"
      : "employees";
  const isUnattributedView = view === "unattributed";
  // Unknown users have no role, so the role dimension doesn't apply there.
  // Derived (rather than reset in the view-change handler) so it also holds
  // when the view changes through the URL, e.g. back/forward navigation.
  const filterDimension: EmployeeFilterDimension =
    isUnattributedView && rawFilterDimension === "role"
      ? "all"
      : rawFilterDimension;
  const handleViewChange = useCallback(
    (next: EmployeeView) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev);
        if (next === "employees") {
          params.delete(VIEW_SEARCH_PARAM);
        } else {
          params.set(VIEW_SEARCH_PARAM, next);
        }
        return params;
      });
      // The selected user/role may not exist in the other view.
      setSelectedFilterValue(null);
    },
    [setSearchParams],
  );

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );
  const rangeLabel = useMemo(() => {
    if (customRange) return customRangeLabel ?? "the selected range";
    return presetRangeLabel(dateRange);
  }, [customRange, customRangeLabel, dateRange]);

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
  const employees = useMemo(() => {
    if (filterDimension === "all" || !selectedFilterValue) return viewEmployees;
    if (filterDimension === "user") {
      return viewEmployees.filter((item) => item.id === selectedFilterValue);
    }
    const roleName = roleNameById.get(selectedFilterValue);
    return viewEmployees.filter((item) =>
      roleName ? item.role === roleName : false,
    );
  }, [viewEmployees, filterDimension, roleNameById, selectedFilterValue]);
  // Unattributed rows carry a placeholder role, so role filtering would
  // always come back empty in that view.
  const filterDimensions = useMemo<EmployeeFilterDimension[]>(
    () =>
      isUnattributedView
        ? EMPLOYEE_FILTER_DIMENSIONS.filter((value) => value !== "role")
        : EMPLOYEE_FILTER_DIMENSIONS,
    [isUnattributedView],
  );
  const filterOptions = useMemo<FilterOption[]>(() => {
    if (filterDimension === "user") {
      return viewEmployees.map((item) => ({
        id: item.id,
        label: item.name,
      }));
    }
    if (filterDimension === "role") {
      return roles.map((role) => ({
        id: role.id,
        label: role.name,
      }));
    }
    return [];
  }, [viewEmployees, filterDimension, roles]);
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

  const handleFilterDimensionChange = (next: EmployeeFilterDimension) => {
    setFilterDimension(next);
    setSelectedFilterValue(null);
  };
  const handlePresetChange = (preset: DateRangePreset) => {
    setDateRange(preset);
    setCustomRange(null);
    setCustomRangeLabel(null);
  };
  const handleCustomRangeChange = (
    rangeFrom: Date,
    rangeTo: Date,
    label?: string,
  ) => {
    setCustomRange({ from: rangeFrom, to: rangeTo });
    setCustomRangeLabel(label ?? null);
  };
  const handleClearCustomRange = () => {
    setCustomRange(null);
    setCustomRangeLabel(null);
  };

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
          <div
            className={cn(
              "flex gap-4 transition-all duration-300",
              isInsightsOpen
                ? "flex-col items-stretch"
                : "flex-row items-center justify-between",
            )}
          >
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
            <div
              className={cn(
                "flex flex-wrap items-center gap-3",
                isInsightsOpen ? "justify-start" : "shrink-0",
              )}
            >
              <EmployeeFilterBar
                view={view}
                onViewChange={handleViewChange}
                dimensions={filterDimensions}
                dimension={filterDimension}
                onDimensionChange={handleFilterDimensionChange}
                selectedValue={selectedFilterValue}
                onValueChange={setSelectedFilterValue}
                options={filterOptions}
                disabled={isLoading}
              />
              <TimeRangePicker
                preset={customRange ? null : dateRange}
                customRange={customRange}
                customRangeLabel={customRangeLabel}
                onPresetChange={handlePresetChange}
                onCustomRangeChange={handleCustomRangeChange}
                onClearCustomRange={handleClearCustomRange}
                disabled={isLoading}
                projectSlug={projectSlug}
              />
            </div>
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
  onSelectUser,
}: {
  employees: Employee[];
  onSelectUser: (employee: Employee) => void;
}) {
  const [page, setPage] = useState(0);
  const [search, setSearch] = useState("");
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
        render: (item) => (
          <span className="text-muted-foreground">{item.lastActivity}</span>
        ),
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
  const handleSearchChange = (value: string) => {
    setSearch(value);
    setPage(0);
  };

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
    <section className="bg-card flex flex-col gap-4">
      <SearchBar
        value={search}
        onChange={handleSearchChange}
        placeholder="Search by name or email..."
      />
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
