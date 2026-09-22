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
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes, useRoutes } from "@/routes";
import { agentSessionHref } from "@/pages/chatLogs/agentSessionLink";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { ruleCategoryLabel } from "@/pages/security/policy-data";
import { MaskedMatch, RevealAllProvider } from "@/pages/security/risk-ui";
import {
  getCategoryForFinding,
  getRuleTitleFallback,
  isJudgeSource,
} from "@/pages/security/risk-utils";
import { useCanReadFindingChat } from "@/pages/security/unmask";
import { FlaggedMessage } from "@/pages/security/watchdog/EvidenceTitle";
import type { RiskOverviewCategory } from "@gram/client/models/components/riskoverviewcategory.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useInfiniteQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
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

/** Every finding this person triggered, newest first, across all their agent ids. */
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
      {(categories.length > 1 || category) && (
        // A dropdown: a dozen categories would overflow a segmented control.
        // Kept while a category is set, even the only one, so it can be
        // cleared.
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
                {`${ruleCategoryLabel(c.category)} (${Number(c.findings).toLocaleString()})`}
              </SelectItem>
            ))}
            {/* A linked category with nothing in this window. */}
            {category && !categories.some((c) => c.category === category) && (
              <SelectItem value={category}>
                {`${ruleCategoryLabel(category)} (0)`}
              </SelectItem>
            )}
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
  // Same rule as Watchdog evidence: chat:read on the finding's own chat.
  const canReadChat = useCanReadFindingChat();
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

  // The endpoint matches rule ids as substrings; keep exact matches only.
  const results = useMemo(
    () =>
      (query.data?.pages.flatMap((page) => page.results) ?? []).filter(
        (result) => !ruleId || result.ruleId === ruleId,
      ),
    [query.data, ruleId],
  );
  // A page that only held those near-misses would show as empty, or as "No
  // findings" with more to load, so skip on to the next one.
  const lastPage = query.data?.pages.at(-1);
  const skipPage =
    !!ruleId &&
    !!lastPage &&
    !lastPage.results.some((result) => result.ruleId === ruleId) &&
    query.hasNextPage;
  const { isFetchingNextPage, fetchNextPage } = query;
  useEffect(() => {
    if (skipPage && !isFetchingNextPage) void fetchNextPage();
  }, [skipPage, isFetchingNextPage, fetchNextPage]);

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
      render: ({ chatId, chatMessageId, chatTitle }) =>
        canReadChat(chatId) ? (
          // Keep these clicks from expanding the row.
          <div
            className="flex min-w-0 items-center gap-1.5"
            onClick={(e) => e.stopPropagation()}
          >
            <button
              type="button"
              title="Open session"
              className="hover:text-foreground truncate text-left text-sm"
              onClick={() => setOpenChat({ chatId, chatMessageId })}
            >
              {chatTitle || "Untitled session"}
            </button>
            <Link
              to={agentSessionHref(routes.agentSessions.href(), chatId)}
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
          <span className="block truncate text-sm">{chatTitle || "-"}</span>
        ),
    },
    {
      key: "match",
      header: "Match",
      width: "1.2fr",
      render: (result) =>
        isJudgeSource(result.source) ? (
          // A judge finding has no matched span; show its rationale.
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
  if (query.isLoading || (skipPage && results.length === 0)) {
    return <SkeletonTable />;
  }
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
        renderExpandedContent={(result) =>
          canReadChat(result.chatId) ? <FindingDetail result={result} /> : null
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
  const categoryName = category ? ruleCategoryLabel(category) : "Flagged";
  // A judge's rule restates its category.
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

/** Mounted collapsed, so the message loads only once the row opens. */
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
