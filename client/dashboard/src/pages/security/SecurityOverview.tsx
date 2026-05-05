import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useRoutes } from "@/routes";
import { Eye, EyeOff, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useRiskListPolicies } from "@gram/client/react-query/index.js";
import { useSdkClient } from "@/contexts/Sdk";
import {
  DETECTION_RULES,
  RULE_CATEGORY_META,
  type RuleCategory,
} from "./policy-data";
import { ChatDetailPanel } from "@/pages/chatLogs/ChatDetailPanel";
import { Drawer, DrawerContent } from "@/components/ui/drawer";

const RULE_ID_TO_CATEGORY = new Map<string, RuleCategory>();
for (const [category, rules] of Object.entries(DETECTION_RULES)) {
  for (const rule of rules) {
    RULE_ID_TO_CATEGORY.set(rule.id, category as RuleCategory);
  }
}

function getCategoryForFinding(
  source: string | undefined,
  ruleId: string | undefined,
): RuleCategory | null {
  if (source === "destructive_tool") return "destructive_tool";
  if (source === "shadow_mcp") return "shadow_mcp";
  if (!ruleId) return null;
  return RULE_ID_TO_CATEGORY.get(ruleId) ?? null;
}

function CategoryBadge({
  source,
  ruleId,
}: {
  source: string | undefined;
  ruleId: string | undefined;
}) {
  const category = getCategoryForFinding(source, ruleId);
  if (!category) return null;
  return (
    <Badge variant="secondary">{RULE_CATEGORY_META[category].label}</Badge>
  );
}

export default function SecurityOverview() {
  return (
    <RequireScope scope="org:admin" level="page">
      <SecurityOverviewContent />
    </RequireScope>
  );
}

function MaskedMatch({ value }: { value: string | undefined }) {
  const [revealed, setRevealed] = useState(false);

  if (!value) return <span>-</span>;

  if (!revealed) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(true);
        }}
      >
        <EyeOff className="h-3 w-3" />
        <span>Click to reveal</span>
      </button>
    );
  }

  return (
    <span className="inline-flex items-center gap-1">
      <span className="font-mono text-xs">
        {value.length > 40
          ? `${value.slice(0, 20)}...${value.slice(-10)}`
          : value}
      </span>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(false);
        }}
      >
        <Eye className="h-3 w-3" />
      </button>
    </span>
  );
}

function SecurityOverviewContent() {
  const routes = useRoutes();
  const client = useSdkClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get("chat_id");
  const setSelectedChatId = useCallback(
    (chatId: string | null) => {
      setSearchParams((prev) => {
        if (chatId) {
          prev.set("chat_id", chatId);
        } else {
          prev.delete("chat_id");
        }
        return prev;
      });
    },
    [setSearchParams],
  );

  const { data: policiesData, isLoading: policiesLoading } =
    useRiskListPolicies();

  const resultsQuery = useInfiniteQuery({
    queryKey: ["risk", "results", "list"],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.list({
        cursor: pageParam,
        limit: pageParam ? 100 : 10,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const chatSummaryQuery = useInfiniteQuery({
    queryKey: ["risk", "results", "byChat"],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.byChat({
        cursor: pageParam,
        limit: pageParam ? 100 : 10,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const policies = useMemo(
    () => policiesData?.policies ?? [],
    [policiesData?.policies],
  );
  const policyMessageById = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of policies) {
      if (p.userMessage && p.userMessage.trim() !== "") {
        m.set(p.id, p.userMessage);
      }
    }
    return m;
  }, [policies]);
  const results = useMemo(
    () => resultsQuery.data?.pages.flatMap((p) => p.results) ?? [],
    [resultsQuery.data],
  );
  const totalFindings =
    resultsQuery.data?.pages[0]?.totalCount ?? results.length;
  const recentChats = useMemo(
    () => chatSummaryQuery.data?.pages.flatMap((p) => p.chats) ?? [],
    [chatSummaryQuery.data],
  );

  const isInitialLoading =
    policiesLoading || resultsQuery.isLoading || chatSummaryQuery.isLoading;

  if (isInitialLoading) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex items-center justify-center py-20">
            <p className="text-muted-foreground text-sm">Loading...</p>
          </div>
        </Page.Body>
      </Page>
    );
  }

  if (policies.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex flex-col items-center justify-center gap-4 py-20">
            <Shield className="text-muted-foreground h-12 w-12" />
            <h2 className="text-lg font-semibold">Risk Analysis</h2>
            <p className="text-muted-foreground max-w-md text-center text-sm">
              Monitor your chat messages for leaked secrets and sensitive data.
              Set up a risk policy to get started.
            </p>
            <Button onClick={() => routes.policyCenter.goTo()}>
              Go to Policy Center
            </Button>
          </div>
        </Page.Body>
      </Page>
    );
  }

  const totalScanned = policies.reduce(
    (max, p) => Math.max(max, p.totalMessages - p.pendingMessages),
    0,
  );

  const hasData = recentChats.length > 0 || results.length > 0;

  return (
    <>
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-lg font-semibold">Risk Overview</h2>
              <p className="text-muted-foreground text-sm">
                Recent findings from risk analysis scans across your project.
              </p>
            </div>
            <Button
              variant="outline"
              onClick={() => routes.policyCenter.goTo()}
            >
              Manage Policies
            </Button>
          </div>

          <div className="mt-4 grid grid-cols-2 gap-4">
            <div className="rounded-lg border p-4">
              <p className="text-muted-foreground text-sm">Events Scanned</p>
              <p className="text-2xl font-bold">
                {totalScanned.toLocaleString()}
              </p>
            </div>
            <div className="rounded-lg border p-4">
              <p className="text-muted-foreground text-sm">Recent Findings</p>
              <p className="text-2xl font-bold">
                {totalFindings.toLocaleString()}
              </p>
            </div>
          </div>

          {hasData ? (
            <>
              {recentChats.length > 0 && (
                <div className="mt-6">
                  <h3 className="mb-2 text-sm font-semibold">Recent Chats</h3>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Chat</TableHead>
                        <TableHead>User</TableHead>
                        <TableHead>Findings</TableHead>
                        <TableHead>Latest Detected</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {recentChats.map((chat) => (
                        <TableRow
                          key={chat.chatId}
                          className="cursor-pointer"
                          onClick={() => setSelectedChatId(chat.chatId)}
                        >
                          <TableCell className="text-muted-foreground max-w-[300px] truncate text-xs">
                            {chat.chatTitle ?? "Untitled"}
                          </TableCell>
                          <TableCell className="text-muted-foreground text-xs">
                            {chat.userId ?? "-"}
                          </TableCell>
                          <TableCell className="font-mono text-xs">
                            {chat.findingsCount}
                          </TableCell>
                          <TableCell className="text-muted-foreground text-xs">
                            {chat.latestDetected
                              ? new Date(chat.latestDetected).toLocaleString()
                              : "-"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {chatSummaryQuery.hasNextPage && (
                    <div className="mt-2 flex justify-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={chatSummaryQuery.isFetchingNextPage}
                        onClick={() => chatSummaryQuery.fetchNextPage()}
                      >
                        {chatSummaryQuery.isFetchingNextPage
                          ? "Loading..."
                          : "Load More"}
                      </Button>
                    </div>
                  )}
                </div>
              )}

              {results.length > 0 && (
                <div className="mt-6">
                  <h3 className="mb-2 text-sm font-semibold">
                    Recent Findings
                  </h3>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Category</TableHead>
                        <TableHead>Rule</TableHead>
                        <TableHead>Chat</TableHead>
                        <TableHead>User</TableHead>
                        <TableHead className="w-[200px]">Match</TableHead>
                        <TableHead className="w-[240px]">Policy Note</TableHead>
                        <TableHead>Detected</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {results.map((result) => {
                        const policyNote = policyMessageById.get(
                          result.policyId,
                        );
                        return (
                          <TableRow
                            key={result.id}
                            className="cursor-pointer"
                            onClick={() => {
                              if (result.chatId) {
                                setSelectedChatId(result.chatId);
                              }
                            }}
                          >
                            <TableCell>
                              <CategoryBadge
                                source={result.source}
                                ruleId={result.ruleId}
                              />
                            </TableCell>
                            <TableCell className="font-mono text-xs">
                              {result.ruleId ?? "-"}
                            </TableCell>
                            <TableCell className="text-muted-foreground max-w-[200px] truncate text-xs">
                              {result.chatTitle ?? "Untitled"}
                            </TableCell>
                            <TableCell className="text-muted-foreground text-xs">
                              {result.userId ?? "-"}
                            </TableCell>
                            <TableCell className="w-[200px] max-w-[200px] truncate">
                              <MaskedMatch value={result.match} />
                            </TableCell>
                            <TableCell
                              className="text-muted-foreground w-[240px] max-w-[240px] truncate text-xs italic"
                              title={policyNote ?? undefined}
                            >
                              {policyNote ?? "-"}
                            </TableCell>
                            <TableCell className="text-muted-foreground text-xs">
                              {result.createdAt
                                ? new Date(result.createdAt).toLocaleString()
                                : "-"}
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                  {resultsQuery.hasNextPage && (
                    <div className="mt-2 flex justify-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={resultsQuery.isFetchingNextPage}
                        onClick={() => resultsQuery.fetchNextPage()}
                      >
                        {resultsQuery.isFetchingNextPage
                          ? "Loading..."
                          : "Load More"}
                      </Button>
                    </div>
                  )}
                </div>
              )}
            </>
          ) : (
            <div className="mt-8 text-center">
              <p className="text-muted-foreground text-sm">
                No findings yet. Findings will appear here as messages are
                analyzed.
              </p>
            </div>
          )}
        </Page.Body>
      </Page>

      <Drawer
        open={!!selectedChatId}
        onOpenChange={(open) => !open && setSelectedChatId(null)}
        direction="right"
      >
        <DrawerContent className="!w-[720px] sm:!max-w-[720px]">
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
    </>
  );
}
