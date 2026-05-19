import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-sidebar";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  AccessMember,
  Role,
  UserSummary,
} from "@gram/client/models/components";
import { useGramContext, useMembers, useRoles } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Info,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { slugify } from "@/lib/constants";
import { Badge } from "@speakeasy-api/moonshine";
import { HooksSetupDialog } from "@/pages/hooks/HooksSetupDialog";

type EmployeeFilterDimension = "all" | "user" | "role";
type FilterOption = { id: string; label: string };

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
  dimension,
  onDimensionChange,
  selectedValue,
  onValueChange,
  options,
  disabled,
}: {
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
        {EMPLOYEE_FILTER_DIMENSIONS.map((value) => {
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

type EmployeeStatus = "enrolled" | "not_enrolled";

type Employee = {
  id: string;
  name: string;
  email: string;
  role: string;
  status: EmployeeStatus;
  tokenCount: number;
  lastActivity: string;
  photoUrl?: string | null;
};

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

export function InsightsEmployeesContent() {
  const client = useGramContext();
  const { orgSlug, projectSlug } = useSlugs();
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
  const [filterDimension, setFilterDimension] =
    useState<EmployeeFilterDimension>("all");
  const [selectedFilterValue, setSelectedFilterValue] = useState<string | null>(
    null,
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
  const memberIds = useMemo(
    () => members.map((member) => member.id),
    [members],
  );
  const usageQuery = useQuery({
    queryKey: [
      "insights",
      "employees",
      "usage",
      from.toISOString(),
      to.toISOString(),
      memberIds,
    ],
    queryFn: () => fetchEmployeeUsage(client, from, to, memberIds),
    enabled: memberIds.length > 0,
    throwOnError: false,
  });
  const usageSummaries = useMemo(
    () => usageQuery.data ?? [],
    [usageQuery.data],
  );
  const isLoading =
    membersLoading ||
    rolesLoading ||
    (memberIds.length > 0 && usageQuery.isLoading);
  const error = membersError ?? usageQuery.error;
  const allEmployees = useMemo(
    () => buildEmployees(members, roles, usageSummaries),
    [members, roles, usageSummaries],
  );
  const roleNameById = useMemo(
    () => new Map(roles.map((role) => [role.id, role.name])),
    [roles],
  );
  const employees = useMemo(() => {
    if (filterDimension === "all" || !selectedFilterValue) return allEmployees;
    if (filterDimension === "user") {
      return allEmployees.filter((item) => item.id === selectedFilterValue);
    }
    const roleName = roleNameById.get(selectedFilterValue);
    return allEmployees.filter((item) =>
      roleName ? item.role === roleName : false,
    );
  }, [allEmployees, filterDimension, roleNameById, selectedFilterValue]);
  const filterOptions = useMemo<FilterOption[]>(() => {
    if (filterDimension === "user") {
      return allEmployees.map((item) => ({
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
  }, [allEmployees, filterDimension, roles]);
  const totalEmployees = employees.length;
  const enrolledEmployees = employees.filter(
    (item) => item.status === "enrolled",
  ).length;
  const notEnrolledEmployees = totalEmployees - enrolledEmployees;
  const totalTokenCount = employees.reduce(
    (sum, item) => sum + item.tokenCount,
    0,
  );
  const employeesBase = `/${orgSlug}/projects/${projectSlug}/insights/employees`;
  const openUser = (name: string) => {
    navigate(`${employeesBase}/${slugify(name)}`);
  };
  const enrollmentRate =
    totalEmployees > 0 ? (enrolledEmployees / totalEmployees) * 100 : 0;
  const prompt =
    "Using the Employees tab context, summarize who is enrolled in this project based on whether they have any Gram token usage.";

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
        subtitle="Ask who is enrolled, who still needs setup, and how Gram adoption is tracking across the team"
        contextInfo={`Project-scoped Employees tab: ${enrolledEmployees} of ${totalEmployees} employees have Gram Hooks activity in ${rangeLabel} and are enrolled; ${notEnrolledEmployees} employees have no Gram Hooks activity and are not enrolled.`}
        suggestions={[
          {
            title: "Enrollment Coverage",
            label: "Who is enrolled?",
            prompt,
          },
          {
            title: "Not Enrolled",
            label: "Who is not enrolled?",
            prompt:
              "Which employees are not enrolled because they have no Gram token usage in this project?",
          },
          {
            title: "Enrollment Summary",
            label: "Summarize enrollment",
            prompt:
              "Summarize project employee enrollment based on whether each employee has Gram token usage.",
          },
          {
            title: "User Usage",
            label: "Show user usage",
            prompt:
              "Show me a table of organization users' Gram usage for the last 30 days, including token counts, last activity, and hook source breakdowns.",
          },
        ]}
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
                  title="Employees"
                  value={totalEmployees}
                  icon="user"
                  accentColor="blue"
                  subtext="Organization members"
                />
                <MetricCard
                  title="Enrolled"
                  value={enrolledEmployees}
                  icon="circle-check"
                  accentColor="green"
                  subtext="Gram activity present"
                />
                <MetricCard
                  title="Not Enrolled"
                  value={notEnrolledEmployees}
                  icon="triangle-alert"
                  accentColor="orange"
                  subtext="No Gram activity found"
                />
                <MetricCard
                  title="Token Count"
                  value={totalTokenCount}
                  icon="gauge"
                  accentColor="purple"
                  subtext={`${enrollmentRate.toFixed(0)}% enrolled`}
                />
              </section>

              <EmployeeTable employees={employees} onSelectUser={openUser} />
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
  onSelectUser: (name: string) => void;
}) {
  const [page, setPage] = useState(0);
  const [search, setSearch] = useState("");
  const filteredEmployees = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return employees;
    return employees.filter(
      (item) =>
        item.name.toLowerCase().includes(query) ||
        item.email.toLowerCase().includes(query),
    );
  }, [employees, search]);
  const totalPages = Math.ceil(filteredEmployees.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageEmployees = filteredEmployees.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );
  const handleSearchChange = (value: string) => {
    setSearch(value);
    setPage(0);
  };

  return (
    <section className="bg-card flex flex-col gap-4 rounded-xl border p-4">
      <SearchBar
        value={search}
        onChange={handleSearchChange}
        placeholder="Search by name or email..."
      />
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-6">
                <span className="flex items-center gap-1">
                  Employee
                  <SimpleTooltip tooltip="Enrollment is inferred by matching the email reported by each AI coding tool to the member's Gram account. Members without a Gram account won't appear as enrolled until they sign up or directory sync is configured.">
                    <Info className="text-muted-foreground size-3 shrink-0" />
                  </SimpleTooltip>
                </span>
              </TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Enrollment</TableHead>
              <TableHead>Token Count</TableHead>
              <TableHead>Last Activity</TableHead>
              <TableHead className="pr-6 text-right">Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageEmployees.length > 0 ? (
              pageEmployees.map((item) => (
                <TableRow
                  key={item.id}
                  className="cursor-pointer"
                  onClick={() => onSelectUser(item.name)}
                >
                  <TableCell className="pl-6">
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
                        <p className="text-muted-foreground text-xs">
                          {item.email}
                        </p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <p className="text-sm">{item.role}</p>
                  </TableCell>
                  <TableCell>
                    <StatusPill status={item.status} />
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-sm">
                      {item.tokenCount.toLocaleString()}
                    </span>
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {item.lastActivity}
                  </TableCell>
                  <TableCell className="pr-6 text-right">
                    <button
                      type="button"
                      className="text-primary hover:text-primary/80 text-sm font-medium underline underline-offset-4"
                      aria-label={`View ${item.name}`}
                      onClick={(event) => {
                        event.stopPropagation();
                        onSelectUser(item.name);
                      }}
                    >
                      View
                    </button>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className="text-muted-foreground py-10 text-center text-sm"
                >
                  {search
                    ? `No employees matching "${search}".`
                    : "No organization members found."}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        {totalPages > 1 && (
          <div className="flex items-center justify-between border-t px-4 py-3">
            <p className="text-muted-foreground text-sm">
              {safePage * PAGE_SIZE + 1}–
              {Math.min((safePage + 1) * PAGE_SIZE, filteredEmployees.length)}{" "}
              of {filteredEmployees.length}
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
      </div>
    </section>
  );
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

function buildEmployees(
  members: AccessMember[],
  roles: Role[],
  summaries: UserSummary[],
): Employee[] {
  const roleNameById = new Map(roles.map((role) => [role.id, role.name]));
  const summaryByUserId = new Map(
    summaries.map((summary) => [summary.userId, summary]),
  );

  return members
    .map((member) => {
      const summary = summaryByUserId.get(member.id);
      const tokenCount =
        (summary?.totalInputTokens ?? 0) + (summary?.totalOutputTokens ?? 0);
      const status: EmployeeStatus =
        summary != null ? "enrolled" : "not_enrolled";

      return {
        id: member.id,
        name: member.name,
        email: member.email,
        role: roleNameById.get(member.roleId) ?? member.roleId,
        status,
        tokenCount,
        photoUrl: member.photoUrl,
        lastActivity: summary
          ? formatUnixNano(summary.lastSeenUnixNano)
          : "No activity found",
      };
    })
    .sort((a, b) => {
      if (a.status !== b.status) {
        return a.status === "not_enrolled" ? -1 : 1;
      }

      return a.name.localeCompare(b.name);
    });
}

async function fetchEmployeeUsage(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userIds: string[],
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
            userIds,
            eventSource: "hook",
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

function formatUnixNano(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);

  return dateTimeFormatters.humanize(new Date(millis));
}
