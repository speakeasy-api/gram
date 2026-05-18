import { LogWorkbench } from "@/components/log-workbench";
import { Drawer, DrawerContent } from "@/components/ui/drawer";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { ChatDetailPanel } from "@/pages/chatLogs/ChatDetailPanel";
import type { RiskResult } from "@gram/client/models/components";
import {
  invalidateAllRiskListResults,
  invalidateAllRiskListShadowMCPApprovals,
  useRiskApproveShadowMCPMutation,
  useRiskListPolicies,
} from "@gram/client/react-query/index.js";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw, ShieldOff } from "lucide-react";
import { useCallback, useMemo, useRef } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { CategoryLabel, MaskedMatch, RuleLabel } from "./risk-ui";

const RISK_EVENTS_GRID =
  "grid grid-cols-[172px_minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1.25fr)_minmax(0,1.1fr)_110px] gap-3";

export default function RiskEvents() {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get("chat_id");
  const policyFilter = searchParams.get("policy_id") ?? "";
  const containerRef = useRef<HTMLDivElement>(null);

  const setSelectedChatId = useCallback(
    (chatId: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (chatId) {
            next.set("chat_id", chatId);
          } else {
            next.delete("chat_id");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setPolicyFilter = useCallback(
    (policyId: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (policyId) {
            next.set("policy_id", policyId);
          } else {
            next.delete("policy_id");
          }
          return next;
        },
        { replace: true },
      );
      containerRef.current?.scrollTo({ top: 0 });
    },
    [setSearchParams],
  );

  const { data: policiesData, isLoading: policiesLoading } =
    useRiskListPolicies();
  const policies = useMemo(
    () => policiesData?.policies ?? [],
    [policiesData?.policies],
  );
  const policyMessageById = useMemo(() => {
    const m = new Map<string, string>();
    for (const policy of policies) {
      if (policy.userMessage && policy.userMessage.trim() !== "") {
        m.set(policy.id, policy.userMessage);
      }
    }
    return m;
  }, [policies]);

  const resultsQuery = useInfiniteQuery({
    queryKey: ["risk", "results", "list", policyFilter],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.list({
        cursor: pageParam,
        limit: 50,
        policyId: policyFilter || undefined,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const results = useMemo(
    () => resultsQuery.data?.pages.flatMap((p) => p.results) ?? [],
    [resultsQuery.data],
  );
  const totalCount = resultsQuery.data?.pages[0]?.totalCount ?? results.length;
  const isInitialLoading = policiesLoading || resultsQuery.isLoading;

  const approveMutation = useRiskApproveShadowMCPMutation();
  const handleExclude = useCallback(
    (policyId: string, match: string, serverName?: string) => {
      approveMutation.mutate(
        {
          request: {
            approveShadowMCPRequestBody: {
              policyId,
              match,
              serverName,
            },
          },
        },
        {
          onSuccess: () => {
            toast.success("Excluded from policy");
            queryClient.invalidateQueries({
              queryKey: ["risk", "results", "list"],
            });
            invalidateAllRiskListResults(queryClient);
            invalidateAllRiskListShadowMCPApprovals(queryClient);
          },
          onError: (err) => {
            toast.error(`Failed to exclude: ${err.message ?? "unknown error"}`);
          },
        },
      );
    },
    [approveMutation, queryClient],
  );

  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const container = e.currentTarget;
      const distanceFromBottom =
        container.scrollHeight - (container.scrollTop + container.clientHeight);

      if (resultsQuery.isFetchingNextPage || resultsQuery.isFetching) return;
      if (!resultsQuery.hasNextPage) return;

      if (distanceFromBottom < 200) {
        resultsQuery.fetchNextPage();
      }
    },
    [resultsQuery],
  );

  return (
    <LogWorkbench
      title="Risk Events"
      description="Review policy findings across recent analyzed chats."
      actions={
        <Button
          variant="secondary"
          size="sm"
          onClick={() => resultsQuery.refetch()}
          disabled={resultsQuery.isFetching}
          aria-label="Refresh risk events"
        >
          <RefreshCw
            className={cn("h-4 w-4", resultsQuery.isFetching && "animate-spin")}
          />
          Refresh
        </Button>
      }
      filters={
        <Select
          value={policyFilter || "all"}
          onValueChange={(value) =>
            setPolicyFilter(value === "all" ? "" : value)
          }
        >
          <SelectTrigger className="w-[260px]">
            <SelectValue placeholder="Filter by policy" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All policies</SelectItem>
            {policies.map((policy) => (
              <SelectItem key={policy.id} value={policy.id}>
                {policy.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
      status={
        resultsQuery.isFetching && results.length > 0 ? (
          <div className="bg-primary/20 h-1 shrink-0">
            <div className="bg-primary h-full animate-pulse" />
          </div>
        ) : null
      }
      header={<RiskEventsHeader />}
      footer={
        results.length > 0 ? (
          <RiskEventsFooter
            count={results.length}
            totalCount={totalCount}
            hasNextPage={resultsQuery.hasNextPage}
            isFetchingNextPage={resultsQuery.isFetchingNextPage}
            onLoadMore={() => resultsQuery.fetchNextPage()}
          />
        ) : null
      }
      detail={
        <Drawer
          open={!!selectedChatId}
          onOpenChange={(open) => !open && setSelectedChatId(null)}
          direction="right"
        >
          <DrawerContent className="data-[vaul-drawer-direction=right]:w-[720px] data-[vaul-drawer-direction=right]:sm:max-w-[720px]">
            {selectedChatId && (
              <ChatDetailPanel
                chatId={selectedChatId}
                resolutions={[]}
                onClose={() => setSelectedChatId(null)}
                onDelete={() => setSelectedChatId(null)}
                collapseNonRisk
              />
            )}
          </DrawerContent>
        </Drawer>
      }
      scrollRef={containerRef}
      onScroll={handleScroll}
    >
      <RiskEventsRows
        error={resultsQuery.error}
        isLoading={isInitialLoading}
        results={results}
        policyMessageById={policyMessageById}
        isExcluding={approveMutation.isPending}
        onSelectChat={setSelectedChatId}
        onExclude={handleExclude}
      />
    </LogWorkbench>
  );
}

function RiskEventsHeader() {
  return (
    <div
      className={cn(
        RISK_EVENTS_GRID,
        "bg-muted/30 text-muted-foreground shrink-0 items-center border-b px-8 py-2.5 text-xs font-medium tracking-wide",
      )}
    >
      <div className="min-w-0">Timestamp</div>
      <div className="min-w-0">Category</div>
      <div className="min-w-0">Rule</div>
      <div className="min-w-0">Session Name</div>
      <div className="min-w-0">User</div>
      <div className="min-w-0">Match</div>
      <div className="min-w-0">Policy Note</div>
      <div className="flex min-w-0 justify-center">Actions</div>
    </div>
  );
}

function RiskEventsRows({
  error,
  isLoading,
  results,
  policyMessageById,
  isExcluding,
  onSelectChat,
  onExclude,
}: {
  error: Error | null;
  isLoading: boolean;
  results: RiskResult[];
  policyMessageById: Map<string, string>;
  isExcluding: boolean;
  onSelectChat: (chatId: string | null) => void;
  onExclude: (policyId: string, match: string, serverName?: string) => void;
}) {
  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 py-12">
        <div className="bg-destructive/10 flex size-12 items-center justify-center rounded-full">
          <Icon name="circle-alert" className="text-destructive size-6" />
        </div>
        <span className="text-foreground font-medium">
          Error loading risk events
        </span>
        <span className="text-muted-foreground max-w-sm text-center text-sm">
          {error.message}
        </span>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Icon name="loader-circle" className="size-5 animate-spin" />
        <span>Loading risk events...</span>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <Icon name="inbox" className="text-muted-foreground size-6" />
        </div>
        <span className="text-foreground font-medium">
          No risk events found
        </span>
        <span className="text-muted-foreground max-w-sm text-sm">
          Findings will appear here as messages are analyzed.
        </span>
      </div>
    );
  }

  return (
    <>
      {results.map((result) => (
        <RiskEventsRow
          key={result.id}
          result={result}
          policyNote={policyMessageById.get(result.policyId)}
          isExcluding={isExcluding}
          onSelectChat={onSelectChat}
          onExclude={onExclude}
        />
      ))}
    </>
  );
}

function RiskEventsRow({
  result,
  policyNote,
  isExcluding,
  onSelectChat,
  onExclude,
}: {
  result: RiskResult;
  policyNote: string | undefined;
  isExcluding: boolean;
  onSelectChat: (chatId: string | null) => void;
  onExclude: (policyId: string, match: string, serverName?: string) => void;
}) {
  const isShadowMCP = result.source === "shadow_mcp";

  return (
    <div
      role={result.chatId ? "button" : undefined}
      tabIndex={result.chatId ? 0 : undefined}
      className={cn(
        RISK_EVENTS_GRID,
        "hover:bg-muted/30 w-full items-center border-b px-8 py-3 text-left text-sm transition-colors",
        !result.chatId && "cursor-default",
      )}
      onClick={() => {
        if (result.chatId) {
          onSelectChat(result.chatId);
        }
      }}
      onKeyDown={(e) => {
        if (!result.chatId) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelectChat(result.chatId);
        }
      }}
    >
      <div className="text-muted-foreground min-w-0 font-mono text-xs">
        {result.createdAt ? new Date(result.createdAt).toLocaleString() : "-"}
      </div>
      <div className="min-w-0 truncate">
        <CategoryLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">
        <RuleLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">{result.chatTitle ?? "Untitled"}</div>
      <div className="min-w-0 truncate">{result.userId ?? "-"}</div>
      <div className="min-w-0 truncate">
        {isShadowMCP && result.match ? (
          <span className="font-mono text-xs" title={result.match}>
            {result.match}
          </span>
        ) : (
          <MaskedMatch value={result.match} />
        )}
      </div>
      <div className="min-w-0 truncate" title={policyNote}>
        {policyNote ?? "-"}
      </div>
      <div className="flex min-w-0 justify-center">
        {isShadowMCP && result.match ? (
          <Button
            variant="tertiary"
            size="sm"
            disabled={isExcluding}
            onClick={(e) => {
              e.stopPropagation();
              onExclude(result.policyId, result.match!);
            }}
            title="Exclude this MCP server from the policy"
          >
            <ShieldOff className="h-3 w-3" />
            <span className="text-xs">Exclude</span>
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function RiskEventsFooter({
  count,
  totalCount,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
}: {
  count: number;
  totalCount: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center justify-between gap-4 border-t px-8 py-3 text-sm">
      <span>
        Showing {count.toLocaleString()} of {totalCount.toLocaleString()}{" "}
        {totalCount === 1 ? "finding" : "findings"}
        {hasNextPage && " - Scroll to load more"}
      </span>
      {hasNextPage ? (
        <Button
          variant="tertiary"
          size="sm"
          disabled={isFetchingNextPage}
          onClick={onLoadMore}
        >
          {isFetchingNextPage ? "Loading..." : "Load More"}
        </Button>
      ) : null}
    </div>
  );
}
