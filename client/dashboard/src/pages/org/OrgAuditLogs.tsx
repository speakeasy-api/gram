import { useQueryState } from "nuqs";
import type { MCPServerEntry } from "@/elements";
import { recommended } from "@/elements/plugins";
import { RequireScope } from "@/components/require-scope";
import { InsightsConfig, InsightsProvider } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Button } from "@/components/ui/button";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Switch } from "@/components/ui/switch";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { internalMcpUrl } from "@/hooks/useToolsetUrl";
import { subjectHref } from "@/components/auditlogs/subject-href";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAuditLogFacets } from "@gram/client/react-query/auditLogFacets.js";
import { useAuditLogsInfinite } from "@gram/client/react-query/auditLogs.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { Icon, Input } from "@speakeasy-api/moonshine";
import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Link } from "react-router";
import {
  formatAuditAction,
  getActorLabel,
  renderVerb,
} from "@/lib/audit-log-format";
import { StructuredDiff } from "@/components/auditlogs/structured-diff";
import {
  ActionBadge,
  ActionDot,
  AuditFeedFooter,
  DateGroupHeader,
  FacetSelect,
} from "@/components/auditlogs/feed";
import {
  formatDateHeader,
  formatTimeOnly,
  groupLogsByDate,
  type FacetOption,
} from "@/lib/audit-log-feed";
import { cn, getServerURL } from "@/lib/utils";

function StrongName({ children }: { children: ReactNode }) {
  return <strong className="text-foreground font-semibold">{children}</strong>;
}

function getSubjectLabel(log: AuditLog) {
  return log.subjectDisplayName || log.subjectSlug || log.subjectId;
}

function recordString(value: unknown, key: string): string | undefined {
  if (value == null || typeof value !== "object") return undefined;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" && field !== "" ? field : undefined;
}

function formatRoleSlug(roleSlug: string) {
  return roleSlug.replace(/[-_]/g, " ");
}

function inviteCreatedRole(log: AuditLog): string | undefined {
  return recordString(log.metadata, "role_slug");
}

function inviteRoleBefore(log: AuditLog): string | undefined {
  return (
    recordString(log.beforeSnapshot, "RoleSlug") ??
    recordString(log.beforeSnapshot, "role_slug")
  );
}

function inviteRoleAfter(log: AuditLog): string | undefined {
  return (
    recordString(log.afterSnapshot, "RoleSlug") ??
    recordString(log.afterSnapshot, "role_slug")
  );
}

function inviteSubjectText(log: AuditLog) {
  const subject = getSubjectLabel(log);

  if (log.action === "organization_invitation:create") {
    const role = inviteCreatedRole(log);
    return role ? `${subject} as ${formatRoleSlug(role)}` : subject;
  }

  if (log.action === "organization_invitation:update_role") {
    const before = inviteRoleBefore(log);
    const after = inviteRoleAfter(log);
    let roleText = "";
    if (before && after && before !== after) {
      roleText = ` from ${formatRoleSlug(before)} to ${formatRoleSlug(after)}`;
    } else if (after) {
      roleText = ` to ${formatRoleSlug(after)}`;
    }

    return `${subject}${roleText}`;
  }

  return subject;
}

function renderInviteSubject(log: AuditLog, monoClass: string) {
  return <span className={monoClass}>{inviteSubjectText(log)}</span>;
}

function truncateMiddle(value: string, start = 18, end = 16) {
  if (value.length <= start + end + 1) {
    return value;
  }
  return `${value.slice(0, start)}...${value.slice(-end)}`;
}

const SUBJECT_MONO_CLASS = "font-mono text-xs text-muted-foreground";

// A subject rendered as a link to its detail page. Centralizes the mono styling
// and hover affordance so every linked subject looks identical.
function SubjectLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to} className={cn(SUBJECT_MONO_CLASS, "hover:underline")}>
      {children}
    </Link>
  );
}

// The text shown for a linked subject. Mirrors the identifier each subject type
// is keyed on — slug where one exists, otherwise id or display name.
function subjectLinkText(log: AuditLog): string {
  switch (log.subjectType) {
    case "deployment":
      return log.subjectId;
    case "toolset":
    case "mcp_server":
    case "environment":
    case "project":
    case "plugin":
    case "mcp_collection":
      return log.subjectSlug || log.subjectId;
    default:
      return getSubjectLabel(log);
  }
}

function renderSubject(log: AuditLog, orgSlug: string) {
  const monoClass = SUBJECT_MONO_CLASS;

  if (log.subjectType === "organization_invitation") {
    return renderInviteSubject(log, monoClass);
  }

  const href = subjectHref(log, orgSlug);
  if (href) {
    return <SubjectLink to={href}>{subjectLinkText(log)}</SubjectLink>;
  }

  if (log.subjectType === "asset") {
    const subjectLabel = getSubjectLabel(log);
    return (
      <SimpleTooltip tooltip={subjectLabel}>
        <span
          className={cn(
            monoClass,
            "inline-block max-w-[34ch] truncate align-bottom",
          )}
        >
          {truncateMiddle(subjectLabel)}
        </span>
      </SimpleTooltip>
    );
  }

  if (
    log.action === "organization:webhooks_enabled" ||
    log.action === "organization:webhooks_disabled"
  ) {
    return null;
  }

  if (log.subjectType === "otel_forwarding_config") {
    // The subject ID is a UUID with no human-meaningful slug or display name.
    // The verb text is self-contained (e.g., "enabled OpenTelemetry forwarding
    // to otel.example.com"), so we omit the subject column entirely.
    return null;
  }

  return <span className={monoClass}>{getSubjectLabel(log)}</span>;
}

function hasDiff(log: AuditLog): boolean {
  if (log.action.startsWith("deployments:")) {
    return false;
  }
  return log.beforeSnapshot != null || log.afterSnapshot != null;
}

function AuditLogRow({
  log,
  orgSlug,
  timestampMode,
  isOdd,
  isHighlighted,
  rowRef,
  highlightMatch,
}: {
  log: AuditLog;
  orgSlug: string;
  timestampMode: "utc" | "local";
  isOdd: boolean;
  isHighlighted?: boolean;
  rowRef?: (el: HTMLDivElement | null) => void;
  highlightMatch?: (text: string) => React.ReactNode;
}) {
  const [diffExpanded, setDiffExpanded] = useState(false);
  const showDiff = hasDiff(log);

  const actorLabel = getActorLabel(log);
  const verbText = renderVerb(log);
  const subjectLink = subjectHref(log, orgSlug);

  const rowContent = (
    <div className="group flex items-start gap-3.5 px-4 py-2.5">
      <ActionDot action={log.action} />
      <ActionBadge action={log.action} />
      <div className="min-w-0 flex-1 text-sm leading-5">
        <span>
          <StrongName>
            {highlightMatch ? highlightMatch(actorLabel) : actorLabel}
          </StrongName>{" "}
          <span className="text-muted-foreground">
            {highlightMatch ? highlightMatch(verbText) : verbText}
          </span>{" "}
          {renderSubject(log, orgSlug)}
        </span>
        {showDiff && (
          <button
            type="button"
            onClick={() => setDiffExpanded((v) => !v)}
            className="ml-2 text-xs text-blue-500 hover:underline"
          >
            {diffExpanded ? "Hide diff ▴" : "Show diff ▾"}
          </button>
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs">
        {formatTimeOnly(log.createdAt, timestampMode)}
      </span>
      {subjectLink && (
        <Link
          to={subjectLink}
          aria-label={`Open ${getSubjectLabel(log)}`}
          className="text-muted-foreground hover:text-foreground focus-visible:text-foreground shrink-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        >
          <Icon name="arrow-right" className="size-4" />
        </Link>
      )}
    </div>
  );

  if (showDiff && diffExpanded) {
    return (
      <div
        ref={rowRef}
        className={cn(isHighlighted && "border-l-foreground border-l-4")}
      >
        <div
          className={cn(
            "rounded-t-lg border border-b-0",
            isOdd ? "bg-muted/30" : "bg-background",
          )}
        >
          {rowContent}
        </div>
        <div className="bg-background rounded-b-lg border border-t-0 px-4 pt-2 pb-3">
          <StructuredDiff log={log} />
        </div>
      </div>
    );
  }

  return (
    <div
      ref={rowRef}
      className={cn(
        "rounded-none transition-colors",
        isOdd ? "bg-muted/30" : "bg-background",
        isHighlighted && "border-l-foreground border-l-4",
      )}
    >
      {rowContent}
    </div>
  );
}

/**
 * Wraps the audit logs page in an InsightsProvider so the AI Insights
 * trigger appears in the breadcrumb bar. Uses the org's first project
 * for Elements session auth (required by the chat API).
 */
function AuditLogsInsightsWrapper({ children }: { children: React.ReactNode }) {
  const organization = useOrganization();
  const { session } = useSession();
  const client = useGramContext();

  const projectSlug = organization.projects[0]?.slug ?? "";
  const { data: toolsetsData, isLoading: isLoadingToolsets } = useListToolsets(
    { gramProject: projectSlug },
    undefined,
    { enabled: Boolean(projectSlug) },
  );

  const getSession = useCallback(async () => {
    const res = await chatSessionsCreate(
      client,
      {
        createRequestBody: {
          embedOrigin: window.location.origin,
        },
      },
      undefined,
      {
        headers: {
          "Gram-Project": projectSlug,
        },
      },
    );
    return res.value?.clientToken ?? "";
  }, [client, projectSlug]);

  const serverURL = getServerURL();

  // Build MCP server entries for all project toolsets
  const mcps = useMemo<MCPServerEntry[] | undefined>(() => {
    if (isLoadingToolsets || !toolsetsData?.toolsets?.length) {
      return undefined;
    }

    return toolsetsData.toolsets.map((toolset) => ({
      url: internalMcpUrl({ slug: projectSlug }, toolset),
      name: toolset.slug,
      environment: toolset.defaultEnvironmentSlug,
    }));
  }, [toolsetsData?.toolsets, projectSlug, isLoadingToolsets]);

  const auditToolsFilter = useCallback(
    ({ toolName }: { toolName: string }) =>
      toolName.includes("audit") ||
      toolName.includes("logs") ||
      toolName.includes("tool_calls"),
    [],
  );

  const mcpConfig = useMemo(
    () => ({
      projectSlug,
      plugins: recommended.except("generative-ui"),
      tools: {
        toolsToInclude: auditToolsFilter,
      },
      api: {
        url: serverURL,
        session: getSession,
        headers: {
          "X-Gram-Source": "dashboard-ai-insights-audit-logs",
        },
      },
      environment: {
        GRAM_SERVER_URL: serverURL,
        GRAM_SESSION_HEADER_GRAM_SESSION: session,
        GRAM_APIKEY_HEADER_GRAM_KEY: "",
        GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT: projectSlug,
      },
      ...(mcps && mcps.length > 0 && { mcps }),
    }),
    [projectSlug, auditToolsFilter, serverURL, getSession, session, mcps],
  );

  return (
    <InsightsProvider
      mcpConfig={mcpConfig}
      title="Audit Log Insights"
      subtitle="Ask about organization activity, changes, and audit events."
      suggestions={INSIGHTS_SUGGESTIONS["org/audit-logs"]}
    >
      {children}
    </InsightsProvider>
  );
}

export default function OrgAuditLogs(): React.JSX.Element {
  const { hasAnyScope } = useRBAC();
  const organization = useOrganization();
  // Only wrap with InsightsProvider when user has org:read or org:admin
  // and at least one project exists (needed for Elements session auth).
  const showInsights =
    hasAnyScope(["org:read", "org:admin"]) && organization.projects.length > 0;

  const page = (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:read" level="page">
          <OrgAuditLogsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );

  if (!showInsights) return page;

  return <AuditLogsInsightsWrapper>{page}</AuditLogsInsightsWrapper>;
}

function OrgAuditLogsInner() {
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

  const tsMode = (timestampMode === "local" ? "local" : "utc") as
    | "utc"
    | "local";

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
  });

  const logs = useMemo(
    () => data?.pages.flatMap((page) => page.result.logs) ?? [],
    [data],
  );

  const dateGroups = useMemo(
    () => groupLogsByDate(logs, tsMode),
    [logs, tsMode],
  );

  // Feed current page state to the AI Insights sidebar as context.
  const insightsContext = useMemo(() => {
    const parts: string[] = [
      "The user is viewing the organization Audit Logs page.",
      `Organization: ${organization.name || orgSlug}`,
    ];
    if (selectedProjectSlug !== "all") {
      parts.push(`Filtered to project: ${selectedProjectSlug}`);
    }
    if (selectedAction !== "all") {
      parts.push(`Filtered to action: ${selectedAction}`);
    }
    if (selectedActor !== "all") {
      parts.push(`Filtered to actor: ${selectedActor}`);
    }
    parts.push(`Currently showing ${logs.length} audit log entries.`);
    if (dateGroups.length > 0) {
      const firstDate = dateGroups[0]!.date!;
      const lastDate = dateGroups[dateGroups.length - 1]!.date!;
      parts.push(
        `Date range: ${formatDateHeader(lastDate, tsMode)} to ${formatDateHeader(firstDate, tsMode)}`,
      );
    }
    return parts.join("\n");
  }, [
    organization.name,
    orgSlug,
    selectedProjectSlug,
    selectedAction,
    selectedActor,
    logs.length,
    dateGroups,
    tsMode,
  ]);

  const logFlatIndices = useMemo(() => {
    const map = new Map<string, number>();
    let idx = 0;
    for (const group of dateGroups) {
      for (const log of group.logs) {
        map.set(log.id, idx++);
      }
    }
    return map;
  }, [dateGroups]);

  const hasActiveFilters =
    selectedProjectSlug !== "all" ||
    selectedAction !== "all" ||
    selectedActor !== "all";

  // --- Search & keyboard navigation state ---
  const [searchQuery, setSearchQuery] = useState("");
  const [currentLogIndex, setCurrentLogIndex] = useState<number | null>(null);
  const [currentSearchIndex, setCurrentSearchIndex] = useState(0);
  const [searchInputFocused, setSearchInputFocused] = useState(false);

  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logRefs = useRef<Map<number, HTMLDivElement>>(new Map());

  // Reset navigation state when the log list changes (filters, pagination)
  useEffect(() => {
    setCurrentLogIndex(null);
  }, [logs]);

  const getSearchableText = useCallback((log: AuditLog): string => {
    const actor = getActorLabel(log);
    const action = formatAuditAction(log.action);
    const verb = renderVerb(log);
    const subject =
      log.subjectType === "organization_invitation"
        ? inviteSubjectText(log)
        : getSubjectLabel(log);
    return `${actor} ${action} ${verb} ${subject}`;
  }, []);

  const deferredSearchQuery = useDeferredValue(searchQuery);

  const searchMatchIndices = useMemo(() => {
    if (!deferredSearchQuery) return [];
    const query = deferredSearchQuery.toLowerCase();
    const indices: number[] = [];
    logs.forEach((log, index) => {
      if (getSearchableText(log).toLowerCase().includes(query)) {
        indices.push(index);
      }
    });
    return indices;
  }, [deferredSearchQuery, logs, getSearchableText]);

  const effectiveSearchIndex =
    searchMatchIndices.length > 0
      ? Math.min(currentSearchIndex, searchMatchIndices.length - 1)
      : 0;

  const scrollToLog = useCallback((index: number) => {
    const element = logRefs.current.get(index);
    if (element) {
      element.scrollIntoView({ behavior: "smooth", block: "center" });
    }
    setCurrentLogIndex(index);
  }, []);

  const navigateToResult = useCallback(
    (direction: "next" | "prev") => {
      if (searchMatchIndices.length === 0) return;

      let newIndex: number;
      if (direction === "next") {
        newIndex = (effectiveSearchIndex + 1) % searchMatchIndices.length;
      } else {
        newIndex =
          effectiveSearchIndex === 0
            ? searchMatchIndices.length - 1
            : effectiveSearchIndex - 1;
      }

      setCurrentSearchIndex(newIndex);
      const targetIndex = searchMatchIndices[newIndex];
      if (targetIndex !== undefined) {
        scrollToLog(targetIndex);
      }
    },
    [effectiveSearchIndex, searchMatchIndices, scrollToLog],
  );

  const handleSearchChange = useCallback(
    (query: string) => {
      setSearchQuery(query);
      setCurrentSearchIndex(0);

      if (query) {
        const q = query.toLowerCase();
        const firstMatch = logs.findIndex((log) =>
          getSearchableText(log).toLowerCase().includes(q),
        );
        if (firstMatch !== -1) {
          scrollToLog(firstMatch);
        }
      } else {
        setCurrentLogIndex(null);
      }
    },
    [logs, getSearchableText, scrollToLog],
  );

  const searchRegex = useMemo(() => {
    if (!searchQuery) return null;
    const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return new RegExp(`(${escaped})`, "gi");
  }, [searchQuery]);

  const highlightMatch = useCallback(
    (text: string): React.ReactNode => {
      if (!searchRegex) return text;

      const parts = text.split(searchRegex);
      return (
        <>
          {parts.map((part, i) =>
            part.toLowerCase() === searchQuery.toLowerCase() ? (
              <mark
                key={i}
                className="bg-yellow-200 text-inherit dark:bg-yellow-800"
              >
                {part}
              </mark>
            ) : (
              part
            ),
          )}
        </>
      );
    },
    [searchQuery, searchRegex],
  );

  // Keyboard handler
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const logsContainer = logsContainerRef.current;
      // Walk through shadow roots to find the real focused element
      let activeElement = document.activeElement;
      while (activeElement?.shadowRoot?.activeElement) {
        activeElement = activeElement.shadowRoot.activeElement;
      }
      const isWithinLogsSection = logsContainer?.contains(
        activeElement as Node,
      );
      const isSearchInputFocusedNow = activeElement?.hasAttribute(
        "data-audit-search-input",
      );

      if (e.key === "Escape") {
        if (isWithinLogsSection || isSearchInputFocusedNow) {
          e.preventDefault();
          setSearchQuery("");
          setCurrentLogIndex(null);
          setCurrentSearchIndex(0);
          const el = document.activeElement as HTMLElement;
          if (el && el.tagName === "INPUT") {
            el.blur();
          }
        }
        return;
      }

      if ((e.metaKey || e.ctrlKey) && e.key === "f") {
        if (isWithinLogsSection || isSearchInputFocusedNow) {
          e.preventDefault();
          const searchInput = document.querySelector<HTMLInputElement>(
            "[data-audit-search-input]",
          );
          searchInput?.focus();
        }
        return;
      }

      const isInInput =
        activeElement?.tagName === "INPUT" ||
        activeElement?.tagName === "TEXTAREA" ||
        activeElement?.tagName === "SELECT" ||
        activeElement?.closest("[contenteditable]") !== null;
      if (!isInInput) {
        switch (e.key) {
          case "/": {
            e.preventDefault();
            const searchInput = document.querySelector<HTMLInputElement>(
              "[data-audit-search-input]",
            );
            searchInput?.focus();
            break;
          }
          case "n":
            if (searchMatchIndices.length > 0) {
              e.preventDefault();
              navigateToResult("next");
            }
            break;
          case "N":
            if (e.shiftKey && searchMatchIndices.length > 0) {
              e.preventDefault();
              navigateToResult("prev");
            }
            break;
          case "j":
            e.preventDefault();
            if (currentLogIndex !== null && currentLogIndex < logs.length - 1) {
              scrollToLog(currentLogIndex + 1);
            } else if (currentLogIndex === null && logs.length > 0) {
              scrollToLog(0);
            }
            break;
          case "k":
            e.preventDefault();
            if (currentLogIndex !== null && currentLogIndex > 0) {
              scrollToLog(currentLogIndex - 1);
            }
            break;
          case "g":
            if (!e.shiftKey && !e.ctrlKey && logs.length > 0) {
              e.preventDefault();
              scrollToLog(0);
            }
            break;
          case "G":
            if (e.shiftKey && logs.length > 0) {
              e.preventDefault();
              scrollToLog(logs.length - 1);
            }
            break;
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    currentLogIndex,
    logs.length,
    navigateToResult,
    scrollToLog,
    searchMatchIndices.length,
  ]);

  return (
    <div className="flex w-full flex-col gap-4">
      <InsightsConfig contextInfo={insightsContext} />
      <div>
        <Heading variant="h4" className="mb-2">
          Recent activity across your organization
        </Heading>
        <Type muted small className="mt-1">
          Review organization-wide and project-level actions in chronological
          order. Search by project, actions, or actor.
        </Type>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <FacetSelect
          label="Project"
          value={selectedProjectSlug}
          onValueChange={(value) => {
            void setSelectedProjectSlug(value);
          }}
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
          onValueChange={(value) => {
            void setSelectedAction(value);
          }}
          placeholder="All actions"
          allLabel="All actions"
          options={actionOptions}
        />
        <FacetSelect
          label="Actor"
          value={selectedActor}
          onValueChange={(value) => {
            void setSelectedActor(value);
          }}
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
              void Promise.allSettled([
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
          <div className="bg-background flex h-8 items-center gap-2 rounded-md border px-3">
            <Type
              small
              className={
                tsMode === "utc" ? "text-foreground" : "text-muted-foreground"
              }
            >
              UTC
            </Type>
            <Switch
              checked={tsMode === "local"}
              onCheckedChange={(checked) => {
                void setTimestampMode(checked ? "local" : "utc");
              }}
              aria-label="Toggle timestamp timezone"
            />
            <Type
              small
              className={
                tsMode === "local" ? "text-foreground" : "text-muted-foreground"
              }
            >
              Local
            </Type>
          </div>
        </div>
      </div>

      <div className="bg-background overflow-hidden rounded-lg border">
        {/* Search toolbar */}
        {!isLoading && !error && logs.length > 0 && (
          <div className="bg-surface/50 flex items-center gap-2 border-b p-2">
            <div className="text-muted-foreground flex items-center gap-3 text-[11px]">
              {searchQuery ? (
                <>
                  <span className="flex items-center gap-1">
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      N
                    </kbd>
                    <span>/</span>
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      ⇧N
                    </kbd>
                    <span className="ml-0.5">results</span>
                  </span>
                  <span className="flex items-center gap-1">
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      ESC
                    </kbd>
                    <span>clear</span>
                  </span>
                </>
              ) : (
                <>
                  <span className="flex items-center gap-1">
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      J
                    </kbd>
                    <span>/</span>
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      K
                    </kbd>
                    <span className="ml-0.5">navigate</span>
                  </span>
                  <span className="flex items-center gap-1">
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      G
                    </kbd>
                    <span>first</span>
                  </span>
                  <span className="flex items-center gap-1">
                    <kbd className="bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                      ⇧G
                    </kbd>
                    <span>last</span>
                  </span>
                </>
              )}
            </div>
            <div className="relative ml-auto">
              <Icon
                name="search"
                className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3 -translate-y-1/2"
              />
              <Input
                data-audit-search-input
                type="text"
                placeholder="Search audit logs"
                value={searchQuery}
                onChange={(e) => handleSearchChange(e.target.value)}
                onFocus={() => setSearchInputFocused(true)}
                onBlur={() => setSearchInputFocused(false)}
                className="w-56 rounded-sm py-1 pr-16 pl-7 text-xs"
              />
              {searchQuery || searchInputFocused ? (
                searchMatchIndices.length > 0 ? (
                  <div className="absolute top-1/2 right-1 flex -translate-y-1/2 items-center gap-0.5">
                    <span className="text-muted-foreground bg-muted rounded-sm px-1 py-0.5 text-[10px]">
                      ESC
                    </span>
                    <span className="text-muted-foreground mx-0.5 text-[10px]">
                      {effectiveSearchIndex + 1}/{searchMatchIndices.length}
                    </span>
                    <div className="flex items-center">
                      <button
                        onClick={() => navigateToResult("prev")}
                        className="hover:bg-muted rounded-sm p-0.5 opacity-60 transition-opacity hover:opacity-100"
                        title="Previous (Shift+N)"
                      >
                        <Icon name="chevron-up" className="size-2.5" />
                      </button>
                      <button
                        onClick={() => navigateToResult("next")}
                        className="hover:bg-muted rounded-sm p-0.5 opacity-60 transition-opacity hover:opacity-100"
                        title="Next (N)"
                      >
                        <Icon name="chevron-down" className="size-2.5" />
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="absolute top-1/2 right-1.5 flex -translate-y-1/2 items-center gap-0.5">
                    <span className="text-muted-foreground bg-muted rounded-sm px-1 py-0.5 text-[10px]">
                      ESC
                    </span>
                    <span className="text-muted-foreground ml-0.5 text-[10px]">
                      0/0
                    </span>
                  </div>
                )
              ) : (
                <div className="absolute top-1/2 right-2 flex -translate-y-1/2 items-center">
                  <span className="text-muted-foreground bg-muted rounded-sm px-1 py-0.5 font-mono text-[10px]">
                    /
                  </span>
                </div>
              )}
            </div>
          </div>
        )}

        <div ref={logsContainerRef} tabIndex={0} className="focus:outline-none">
          {isLoading ? (
            <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
              <Icon name="loader-circle" className="size-4 animate-spin" />
              <span>Loading audit logs...</span>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center gap-2 py-12 text-center">
              <Type className="font-medium">Error loading audit logs</Type>
              <Type muted small>
                {error.message}
              </Type>
            </div>
          ) : logs.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-12 text-center">
              <Type className="font-medium">No audit logs found</Type>
              <Type muted small>
                {selectedProjectSlug === "all" &&
                selectedAction === "all" &&
                selectedActor === "all"
                  ? "Activity will appear here as changes are made across your organization."
                  : "No audit logs match the selected filters."}
              </Type>
            </div>
          ) : (
            <div>
              {dateGroups.map((group) => (
                <React.Fragment key={group.key}>
                  <DateGroupHeader date={group.date} mode={tsMode} />
                  {group.logs.map((log, rowIndex) => {
                    const idx = logFlatIndices.get(log.id) ?? 0;
                    return (
                      <AuditLogRow
                        key={log.id}
                        log={log}
                        orgSlug={orgSlug ?? ""}
                        timestampMode={tsMode}
                        isOdd={rowIndex % 2 === 1}
                        isHighlighted={idx === currentLogIndex}
                        rowRef={(el) => {
                          if (el) logRefs.current.set(idx, el);
                          else logRefs.current.delete(idx);
                        }}
                        highlightMatch={
                          searchQuery ? highlightMatch : undefined
                        }
                      />
                    );
                  })}
                </React.Fragment>
              ))}
            </div>
          )}
        </div>

        <AuditFeedFooter
          count={logs.length}
          hasNextPage={hasNextPage ?? false}
          isFetching={isFetching}
          isFetchingNextPage={isFetchingNextPage}
          onLoadMore={() => {
            void fetchNextPage();
          }}
        />
      </div>
    </div>
  );
}
