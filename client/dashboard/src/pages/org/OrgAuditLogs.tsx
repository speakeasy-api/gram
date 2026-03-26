import { useQueryState } from "nuqs";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Switch } from "@/components/ui/switch";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { AuditLog } from "@gram/client/models/components";
import {
  useAuditLogsInfinite,
  useAuditLogFacets,
} from "@gram/client/react-query";
import { Icon } from "@speakeasy-api/moonshine";
import React, { Suspense, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { HighlightProvider } from "@/components/diffs/provider";

const StaticDiff = React.lazy(() =>
  import("@/components/auditlogs/diff").then((mod) => ({
    default: mod.StaticDiff,
  })),
);

type FacetOption = {
  count?: number;
  displayName: string;
  value: string;
};

function getTimestampFormatter(mode: "utc" | "local") {
  return new Intl.DateTimeFormat(undefined, {
    ...(mode === "utc" ? { timeZone: "UTC" } : {}),
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
    hour12: false,
  });
}

function StrongName({ children }: { children: ReactNode }) {
  return <strong className="font-semibold text-foreground">{children}</strong>;
}

function formatTimestamp(date: Date, mode: "utc" | "local") {
  const formatter = getTimestampFormatter(mode);
  const parts = formatter.formatToParts(date);
  const values = Object.fromEntries(
    parts.map((part) => [part.type, part.value]),
  );

  return `${values.day}/${values.month}/${values.year} ${values.hour}:${values.minute}:${values.second} ${values.timeZoneName}`;
}

function getActorLabel(log: AuditLog) {
  return log.actorDisplayName || log.actorSlug || "Someone";
}

function getSubjectLabel(log: AuditLog) {
  return log.subjectDisplayName || log.subjectSlug || log.subjectId;
}

function truncateMiddle(value: string, start = 18, end = 16) {
  if (value.length <= start + end + 1) {
    return value;
  }

  return `${value.slice(0, start)}...${value.slice(-end)}`;
}

function renderSubject(log: AuditLog, orgSlug: string) {
  if (log.subjectType === "deployment" && log.projectSlug) {
    return (
      <Link
        to={`/${orgSlug}/projects/${log.projectSlug}/deployments/${log.subjectId}`}
        className="text-primary hover:underline"
      >
        <StrongName>{log.subjectId}</StrongName>
      </Link>
    );
  }

  if (log.subjectType === "toolset" && log.projectSlug && log.subjectSlug) {
    return (
      <Link
        to={`/${orgSlug}/projects/${log.projectSlug}/mcp/${log.subjectSlug}`}
        className="text-primary hover:underline"
      >
        <StrongName>{log.subjectSlug}</StrongName>
      </Link>
    );
  }

  if (log.subjectType === "project" && log.subjectSlug) {
    return (
      <Link
        to={`/${orgSlug}/projects/${log.subjectSlug}`}
        className="text-primary hover:underline"
      >
        <StrongName>{log.subjectSlug}</StrongName>
      </Link>
    );
  }

  if (log.subjectType === "api_key") {
    return (
      <Link
        to={`/${orgSlug}/api-keys`}
        className="text-primary hover:underline"
      >
        <StrongName>{getSubjectLabel(log)}</StrongName>
      </Link>
    );
  }

  if (log.subjectType === "asset") {
    const subjectLabel = getSubjectLabel(log);

    return (
      <SimpleTooltip tooltip={subjectLabel}>
        <span className="inline-block max-w-[34ch] align-bottom">
          <StrongName>{truncateMiddle(subjectLabel)}</StrongName>
        </span>
      </SimpleTooltip>
    );
  }

  return <StrongName>{getSubjectLabel(log)}</StrongName>;
}

function getResourceLabel(resource: string) {
  switch (resource) {
    case "api_key":
      return "API key";
    case "asset":
      return "asset";
    case "custom_domains":
      return "custom domain";
    case "deployments":
      return "deployment";
    case "environment":
      return "environment";
    case "mcp_metadata":
      return "MCP metadata";
    case "project":
      return "project";
    case "template":
      return "template";
    case "toolset":
      return "MCP server";
    case "variation":
      return "global variation";
    default:
      return resource.replace(/_/g, " ");
  }
}

function formatAuditAction(action: string) {
  const [resource, verb] = action.split(":");

  if (!resource || !verb) {
    return action;
  }

  const resourceLabel = resource === "toolset" ? "mcp" : resource;

  return `${resourceLabel}:${verb}`;
}

function FacetSelect({
  label,
  value,
  onValueChange,
  placeholder,
  allLabel,
  options,
}: {
  label: string;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
  allLabel: string;
  options: Array<
    Pick<FacetOption, "displayName" | "value"> & {
      count?: number;
    }
  >;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Type small muted>
        {label}
      </Type>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger size="sm" className="min-w-[220px] bg-background">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{allLabel}</SelectItem>
          {options.map((option) => (
            <SelectItem
              key={option.value}
              value={option.value}
              description={
                option.count == null
                  ? undefined
                  : `${option.count.toLocaleString()} audit log${option.count === 1 ? "" : "s"}`
              }
            >
              {option.displayName}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function renderAuditMessage(log: AuditLog, orgSlug: string) {
  const actor = <StrongName>{getActorLabel(log)}</StrongName>;
  const subject = renderSubject(log, orgSlug);

  switch (log.action) {
    case "project:create":
      return (
        <>
          {actor} created project {subject}
        </>
      );
    case "project:update":
      return (
        <>
          {actor} updated project {subject}
        </>
      );
    case "project:delete":
      return (
        <>
          {actor} deleted project {subject}
        </>
      );
    case "environment:create":
      return (
        <>
          {actor} created environment {subject}
        </>
      );
    case "environment:update":
      return (
        <>
          {actor} updated environment {subject}
        </>
      );
    case "environment:delete":
      return (
        <>
          {actor} deleted environment {subject}
        </>
      );
    case "template:create":
      return (
        <>
          {actor} created template {subject}
        </>
      );
    case "template:update":
      return (
        <>
          {actor} updated template {subject}
        </>
      );
    case "template:delete":
      return (
        <>
          {actor} deleted template {subject}
        </>
      );
    case "toolset:create":
      return (
        <>
          {actor} created MCP server {subject}
        </>
      );
    case "toolset:update":
      return (
        <>
          {actor} updated MCP server {subject}
        </>
      );
    case "toolset:delete":
      return (
        <>
          {actor} deleted MCP server {subject}
        </>
      );
    case "toolset:attach_external_oauth":
      return (
        <>
          {actor} attached an external OAuth server to MCP server {subject}
        </>
      );
    case "toolset:detach_external_oauth":
      return (
        <>
          {actor} detached an external OAuth server from MCP server {subject}
        </>
      );
    case "toolset:attach_oauth_proxy":
      return (
        <>
          {actor} attached an OAuth proxy to MCP server {subject}
        </>
      );
    case "toolset:detach_oauth_proxy":
      return (
        <>
          {actor} detached an OAuth proxy from MCP server {subject}
        </>
      );
    case "api_key:create":
      return (
        <>
          {actor} created API key {subject}
        </>
      );
    case "api_key:revoke":
      return (
        <>
          {actor} revoked API key {subject}
        </>
      );
    case "variation:update_global":
      return (
        <>
          {actor} updated a global variation for {subject}
        </>
      );
    case "variation:delete_global":
      return (
        <>
          {actor} deleted a global variation for {subject}
        </>
      );
    case "deployments:create":
      return (
        <>
          {actor} created deployment {subject}
        </>
      );
    case "deployments:evolve":
      return (
        <>
          {actor} created deployment {subject}
        </>
      );
    case "deployments:redeploy":
      return (
        <>
          {actor} redeployed deployment {subject}
        </>
      );
    case "custom_domains:create":
      return (
        <>
          {actor} added custom domain {subject}
        </>
      );
    case "custom_domains:delete":
      return (
        <>
          {actor} deleted custom domain {subject}
        </>
      );
    case "mcp_metadata:update":
      return (
        <>
          {actor} updated MCP metadata for {subject}
        </>
      );
    case "asset:create":
      return (
        <>
          {actor} uploaded asset {subject}
        </>
      );
    default: {
      const [resource = "activity", verb = "updated"] = log.action.split(":");

      return (
        <>
          {actor} {verb.replace(/_/g, " ")} {getResourceLabel(resource)}{" "}
          {subject}
        </>
      );
    }
  }
}

export default function OrgAuditLogs() {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const [selectedProjectSlug, setSelectedProjectSlug] = useQueryState(
    "project",
    { defaultValue: "all" },
  );
  const [selectedAction, setSelectedAction] = useQueryState("action", {
    defaultValue: "all",
  });
  const [selectedActor, setSelectedActor] = useQueryState("actor", {
    defaultValue: "all",
  });
  const [timestampMode, setTimestampMode] = useQueryState("time", {
    defaultValue: "utc",
  });

  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.slug.localeCompare(b.slug)),
    [organization.projects],
  );

  const { data: facetsData } = useAuditLogFacets!({
    projectSlug:
      selectedProjectSlug === "all" ? undefined : selectedProjectSlug,
  });

  const actionOptions: Array<FacetOption> = useMemo(
    () =>
      (facetsData?.actions ?? []).map((option) => ({
        ...option,
        displayName: formatAuditAction(option.value),
      })),
    [facetsData?.actions],
  );
  const actorOptions: Array<FacetOption> = facetsData?.actors ?? [];

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    isLoading,
  } = useAuditLogsInfinite({
    projectSlug:
      selectedProjectSlug === "all" ? undefined : selectedProjectSlug,
    action: selectedAction === "all" ? undefined : selectedAction,
    actorId: selectedActor === "all" ? undefined : selectedActor,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any);

  const logs = useMemo(
    () => data?.pages.flatMap((page) => page.result.logs) ?? [],
    [data],
  );

  const hasActiveFilters =
    selectedProjectSlug !== "all" ||
    selectedAction !== "all" ||
    selectedActor !== "all";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Audit Logs</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <div className="flex w-full flex-col gap-4">
          <div>
            <Type className="font-medium">Recent activity across Gram</Type>
            <Type muted small className="mt-1">
              Review organization-wide and project-level actions in
              chronological order.
            </Type>
          </div>

          <div className="flex flex-wrap items-end gap-3">
            <FacetSelect
              label="Project"
              value={selectedProjectSlug}
              onValueChange={setSelectedProjectSlug}
              placeholder="All projects"
              allLabel="All projects"
              options={projects.map((project) => ({
                value: project.slug,
                displayName: project.slug,
              }))}
            />
            <FacetSelect
              label="Action"
              value={selectedAction}
              onValueChange={setSelectedAction}
              placeholder="All actions"
              allLabel="All actions"
              options={actionOptions}
            />
            <FacetSelect
              label="Actor"
              value={selectedActor}
              onValueChange={setSelectedActor}
              placeholder="All actors"
              allLabel="All actors"
              options={actorOptions}
            />
            <div className="flex flex-col gap-1.5">
              <Type small muted>
                Filters
              </Type>
              <Button
                variant="outline"
                size="sm"
                disabled={!hasActiveFilters}
                onClick={() => {
                  Promise.allSettled([
                    setSelectedProjectSlug("all"),
                    setSelectedAction("all"),
                    setSelectedActor("all"),
                  ]);
                }}
              >
                Clear filters
              </Button>
            </div>
            <div className="flex flex-col gap-1.5">
              <Type small muted>
                Timestamp
              </Type>
              <div className="flex h-8 items-center gap-2 rounded-md border bg-background px-3">
                <Type
                  small
                  className={
                    timestampMode === "utc"
                      ? "text-foreground"
                      : "text-muted-foreground"
                  }
                >
                  UTC
                </Type>
                <Switch
                  checked={timestampMode === "local"}
                  onCheckedChange={(checked) => {
                    void setTimestampMode(checked ? "local" : "utc");
                  }}
                  aria-label="Toggle timestamp timezone"
                />
                <Type
                  small
                  className={
                    timestampMode === "local"
                      ? "text-foreground"
                      : "text-muted-foreground"
                  }
                >
                  Local
                </Type>
              </div>
            </div>
          </div>

          <div className="overflow-hidden rounded-lg border bg-background">
            <HighlightProvider>
              <Table>
                <TableHeader className="bg-muted/30">
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="h-11 w-[240px] text-xs uppercase tracking-wide text-muted-foreground">
                      Timestamp
                    </TableHead>
                    <TableHead className="h-11 w-[180px] text-xs uppercase tracking-wide text-muted-foreground">
                      Project
                    </TableHead>
                    <TableHead className="h-11 text-xs uppercase tracking-wide text-muted-foreground">
                      Event
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {isLoading ? (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={3} className="py-12">
                        <div className="flex items-center justify-center gap-2 text-muted-foreground">
                          <Icon
                            name="loader-circle"
                            className="size-4 animate-spin"
                          />
                          <span>Loading audit logs...</span>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : error ? (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={3} className="py-12">
                        <div className="flex flex-col items-center gap-2 text-center">
                          <Type className="font-medium">
                            Error loading audit logs
                          </Type>
                          <Type muted small>
                            {error.message}
                          </Type>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : logs.length === 0 ? (
                    <TableRow className="hover:bg-transparent">
                      <TableCell colSpan={3} className="py-12">
                        <div className="flex flex-col items-center gap-2 text-center">
                          <Type className="font-medium">
                            No audit logs found
                          </Type>
                          <Type muted small>
                            {selectedProjectSlug === "all" &&
                            selectedAction === "all" &&
                            selectedActor === "all"
                              ? "Activity will appear here as changes are made across your organization."
                              : "No audit logs match the selected filters."}
                          </Type>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : (
                    logs.map((log) => {
                      return (
                        <TableRow key={log.id}>
                          <TableCell className="font-mono text-xs text-muted-foreground">
                            {formatTimestamp(
                              log.createdAt,
                              timestampMode === "local" ? "local" : "utc",
                            )}
                          </TableCell>
                          <TableCell className="font-mono text-sm">
                            {log.projectSlug ? (
                              <Link
                                to={`/${orgSlug}/projects/${log.projectSlug}`}
                                className="text-primary hover:underline"
                              >
                                {log.projectSlug}
                              </Link>
                            ) : null}
                          </TableCell>
                          <TableCell className="whitespace-normal text-sm leading-6">
                            <div className="flex flex-nowrap items-baseline gap-2">
                              <Badge
                                variant="outline"
                                className="font-mono text-[11px]"
                              >
                                {formatAuditAction(log.action)}
                              </Badge>
                              <span>{renderAuditMessage(log, orgSlug)}</span>
                            </div>
                            {renderDiff(log)}
                          </TableCell>
                        </TableRow>
                      );
                    })
                  )}
                </TableBody>
              </Table>
            </HighlightProvider>

            {(logs.length > 0 || isFetchingNextPage) && (
              <div className="flex items-center justify-between border-t bg-muted/20 px-4 py-3">
                <Type muted small>
                  {logs.length.toLocaleString()} audit log
                  {logs.length === 1 ? "" : "s"}
                </Type>

                {hasNextPage ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => fetchNextPage()}
                    disabled={isFetchingNextPage}
                  >
                    {isFetchingNextPage ? (
                      <>
                        <Icon
                          name="loader-circle"
                          className="size-4 animate-spin"
                        />
                        Loading...
                      </>
                    ) : (
                      "Load more"
                    )}
                  </Button>
                ) : (
                  <Type muted small>
                    {isFetching ? "Refreshing..." : "End of audit log history"}
                  </Type>
                )}
              </div>
            )}
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

function renderDiff(log: AuditLog) {
  if (log.action.startsWith("deployments:")) {
    return null;
  }

  if (log.beforeSnapshot == null && log.afterSnapshot == null) {
    return null;
  }

  return <AuditLogDiff log={log} />;
}

function AuditLogDiff({ log }: { log: AuditLog }) {
  const [isVisible, setIsVisible] = useState(false);

  return (
    <div className="mt-2">
      <Button
        className="font-bold"
        type="button"
        variant="link"
        size="sm"
        onClick={() => setIsVisible((visible) => !visible)}
        aria-expanded={isVisible}
      >
        {isVisible ? "Hide diff" : "Show diff"}
      </Button>

      {isVisible ? (
        <div className="mt-2">
          <Suspense
            fallback={
              <div className="flex items-center gap-2 text-muted-foreground">
                <Icon name="loader-circle" className="size-4 animate-spin" />
                <span>Loading diff...</span>
              </div>
            }
          >
            <StaticDiff log={log} />
          </Suspense>
        </div>
      ) : null}
    </div>
  );
}
