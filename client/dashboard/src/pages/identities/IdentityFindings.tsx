import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { useTable } from "@/components/ui/Table/context/context";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes, useRoutes } from "@/routes";
import { agentSessionHref } from "@/pages/chatLogs/agentSessionLink";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import {
  RULE_CATEGORY_META,
  type RuleCategory,
} from "@/pages/security/policy-data";
import { MaskedMatch, RevealAllProvider } from "@/pages/security/risk-ui";
import {
  getCategoryForFinding,
  getRuleTitleFallback,
  isJudgeSource,
} from "@/pages/security/risk-utils";
import { REVEAL_SCOPE } from "@/pages/security/unmask";
import { FlaggedMessage } from "@/pages/security/watchdog/EvidenceTitle";
import type { RiskOverviewCategory } from "@gram/client/models/components/riskoverviewcategory.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useInfiniteQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useLocation, useSearchParams } from "react-router";
import { identityHandoffs } from "./identityHandoffs";
import {
  CATEGORY_PARAM,
  type FindingsFilter,
  RISK_UNAVAILABLE,
  RULE_PARAM,
  setFindingsFilterParams,
} from "./identityFindingsLink";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  riskMatchedOnLabel,
  useCanReadRisk,
  useIdentityPrincipalUrn,
  useIdentityProject,
  useIdentityRisk,
  useIdentityWindow,
} from "./useIdentityQueries";

const PAGE_SIZE = 20;
const ALL = "all";

/**
 * Every finding this person triggered, newest first. Each row expands to the
 * message that was flagged. Matched on every identifier the identity reports,
 * so a person known by more than one agent id sees all of their findings.
 */
export default function IdentityFindingsPage(): JSX.Element {
  const canReadRisk = useCanReadRisk();
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const principalUrn = useIdentityPrincipalUrn(identity);
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    principalUrn,
    new URLSearchParams(location.search),
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const ruleId = searchParams.get(RULE_PARAM) ?? undefined;
  // A rule replaces a category, even in a hand-edited address carrying both.
  const filter: FindingsFilter = ruleId
    ? { ruleId }
    : { category: searchParams.get(CATEGORY_PARAM) ?? undefined };
  const setFilter = (next: FindingsFilter) =>
    setSearchParams(
      (prev) => setFindingsFilterParams(new URLSearchParams(prev), next),
      { replace: true },
    );

  const riskQuery = useIdentityRisk(identity, from, to);
  const categories = riskQuery.data?.categories ?? [];
  const findings = categories.reduce((sum, c) => sum + Number(c.findings), 0);

  // Reached by address: the nav leaves the entry out without org:admin.
  if (!canReadRisk) {
    return (
      <IdentitySection title="Findings">
        <IdentityPanel title="Findings">
          <IdentityPanelEmpty>{RISK_UNAVAILABLE}</IdentityPanelEmpty>
        </IdentityPanel>
      </IdentitySection>
    );
  }

  return (
    <IdentitySection
      title="Findings"
      meta={sectionMeta([{ count: findings, singular: "finding" }])}
      action={
        <FindingsFilterControls
          categories={categories}
          filter={filter}
          onFilterChange={setFilter}
          riskEventsHref={handoffs.riskEvents}
        />
      }
    >
      <FindingsTable
        externalUserIds={identity.externalUserIds}
        from={from}
        to={to}
        filter={filter}
      />
    </IdentitySection>
  );
}

function FindingsFilterControls({
  categories,
  filter,
  onFilterChange,
  riskEventsHref,
}: {
  categories: RiskOverviewCategory[];
  filter: FindingsFilter;
  onFilterChange: (filter: FindingsFilter) => void;
  riskEventsHref: string;
}): JSX.Element {
  const { category, ruleId } = filter;
  return (
    <div className="flex items-center gap-2">
      {ruleId && (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => onFilterChange({})}
          aria-label={`Clear the ${getRuleTitleFallback(ruleId)} filter`}
        >
          <Button.Text>{getRuleTitleFallback(ruleId)}</Button.Text>
          <X className="size-3.5" />
        </Button>
      )}
      {categories.length > 1 && (
        // A dropdown rather than segments: a person can trip a dozen
        // categories, which would run a segmented track off the page.
        <Select
          value={category ?? ALL}
          onValueChange={(value) =>
            onFilterChange(value === ALL ? {} : { category: value })
          }
        >
          <SelectTrigger className="h-8 w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All categories</SelectItem>
            {categories.map((c) => (
              <SelectItem key={c.category} value={c.category}>
                {`${categoryLabel(c.category)} (${Number(c.findings).toLocaleString()})`}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Link
        to={riskEventsHref}
        className="text-muted-foreground hover:text-foreground text-sm whitespace-nowrap"
      >
        Open in Risk Events
      </Link>
    </div>
  );
}

function FindingsTable({
  externalUserIds,
  from,
  to,
  filter,
}: {
  externalUserIds: string[];
  from: Date;
  to: Date;
  filter: FindingsFilter;
}): JSX.Element {
  const client = useSdkClient();
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  // The Watchdog evidence rule: a session's transcript, links and flagged
  // message need chat:read on that chat. Without it the title is plain text
  // and the chat is never requested.
  const canReadChat = (
    result: RiskResult,
  ): result is RiskResult & {
    chatId: string;
  } => Boolean(result.chatId) && hasScope(REVEAL_SCOPE, result.chatId);
  const { slug: gramProject } = useIdentityProject();
  const [openChat, setOpenChat] = useState<{
    chatId: string;
    chatMessageId: string | undefined;
  } | null>(null);
  const { category, ruleId } = filter;

  const query = useInfiniteQuery({
    queryKey: [
      "risk",
      "results",
      "list",
      "identity",
      gramProject,
      externalUserIds,
      category,
      ruleId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: ({ pageParam }) =>
      client.risk.results.list({
        cursor: pageParam,
        limit: PAGE_SIZE,
        externalUserIds,
        category,
        ruleId,
        from,
        to,
        gramProject,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled: externalUserIds.length > 0,
  });

  // The endpoint matches rule ids as substrings, so one rule id that prefixes
  // another would pull in the other's findings. Kept exact here.
  const results = useMemo(
    () =>
      (query.data?.pages.flatMap((page) => page.results) ?? []).filter(
        (result) => !ruleId || result.ruleId === ruleId,
      ),
    [query.data, ruleId],
  );

  const columns: Column<RiskResult>[] = [
    {
      key: "createdAt",
      header: "When",
      width: "170px",
      render: (result) => (
        <span className="text-muted-foreground font-mono text-xs">
          <HumanizeDateTime date={result.createdAt} />
        </span>
      ),
    },
    {
      key: "rule",
      header: "Rule",
      width: "1fr",
      render: (result) => <RuleCell result={result} />,
    },
    {
      key: "session",
      header: "Session",
      width: "1.2fr",
      render: (result) =>
        canReadChat(result) ? (
          // Its own clicks, not the row's: the row click expands the message.
          <div
            className="flex min-w-0 items-center gap-1.5"
            onClick={(e) => e.stopPropagation()}
          >
            <button
              type="button"
              title="Open session"
              className="hover:text-foreground truncate text-left text-sm"
              onClick={() =>
                setOpenChat({
                  chatId: result.chatId,
                  chatMessageId: result.chatMessageId,
                })
              }
            >
              {result.chatTitle || "Untitled session"}
            </button>
            <Link
              to={agentSessionHref(routes.agentSessions.href(), result.chatId)}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Open session in Agent Sessions"
              title="Open in Agent Sessions"
              className="text-muted-foreground hover:text-foreground shrink-0"
            >
              <Icon name="external-link" className="size-3.5" />
            </Link>
          </div>
        ) : (
          <span className="block truncate text-sm">
            {result.chatTitle || "-"}
          </span>
        ),
    },
    {
      key: "match",
      header: "Match",
      width: "1.2fr",
      render: (result) =>
        isJudgeSource(result.source) ? (
          // A judge finding's match is the whole event; its rationale says
          // what it saw, and the expanded row shows the message itself.
          <span className="text-muted-foreground line-clamp-2 text-xs">
            {result.description}
          </span>
        ) : (
          <div className="min-w-0" onClick={(e) => e.stopPropagation()}>
            <MaskedMatch
              resultId={result.id}
              matchRedacted={result.matchRedacted}
              chatId={result.chatId}
            />
          </div>
        ),
    },
  ];

  if (externalUserIds.length === 0) {
    return (
      <IdentityPanel title="Findings">
        <IdentityPanelEmpty>
          This identity reports no agent identifier, so there are no findings to
          list.
        </IdentityPanelEmpty>
      </IdentityPanel>
    );
  }
  if (query.isLoading) return <SkeletonTable />;
  if (query.isError && results.length === 0) {
    return (
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-sm">
          Failed to load findings.
        </span>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void query.refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  }

  return (
    <RevealAllProvider>
      <Table
        columns={columns}
        data={results}
        rowKey={(result) => result.id}
        // No expander without chat:read on the session: there is no message
        // this viewer may load.
        renderExpandedContent={(result) =>
          canReadChat(result) ? <FindingDetail result={result} /> : null
        }
        hasMore={query.hasNextPage}
        onLoadMore={async () => {
          await query.fetchNextPage();
        }}
        noResultsMessage="No findings in this window."
      />
      <p className="text-muted-foreground mt-2 text-xs">
        {riskMatchedOnLabel(externalUserIds)}
      </p>
      <ChatDetailSheet
        chatId={openChat?.chatId ?? null}
        focusedMessageId={openChat?.chatMessageId}
        onClose={() => setOpenChat(null)}
        onDelete={() => setOpenChat(null)}
        riskFocus
      />
    </RevealAllProvider>
  );
}

function RuleCell({ result }: { result: RiskResult }): JSX.Element {
  const category = getCategoryForFinding(result.source, result.ruleId);
  const categoryName = category ? categoryLabel(category) : "Flagged";
  // A judge finding's single rule restates its category, so it gets one line.
  const judge = isJudgeSource(result.source);
  return (
    <div className="min-w-0">
      <div className="truncate text-sm">
        {judge ? categoryName : getRuleTitleFallback(result.ruleId)}
      </div>
      {!judge && (
        <div className="text-muted-foreground truncate text-xs">
          {categoryName}
        </div>
      )}
    </div>
  );
}

/**
 * The expanded row. Mounted with the row but collapsed, so the message is only
 * requested once the row is open.
 */
function FindingDetail({ result }: { result: RiskResult }): JSX.Element {
  const { expandedRowKeys } = useTable();
  const open = expandedRowKeys.has(result.id);
  return (
    <div className="border-border flex flex-col gap-2 border-t px-4 py-3">
      <p className="text-eyebrow">Flagged message</p>
      <FlaggedMessage
        chatId={result.chatId}
        chatMessageId={result.chatMessageId}
        enabled={open}
      />
      <p className="text-muted-foreground font-mono text-xs">
        Confidence {(result.confidence ?? 0).toFixed(2)}
      </p>
    </div>
  );
}

function categoryLabel(category: string): string {
  return RULE_CATEGORY_META[category as RuleCategory]?.label ?? category;
}
