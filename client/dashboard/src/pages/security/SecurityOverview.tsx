import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
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
import { MetricCard } from "@/components/chart/MetricCard";
import { Button as MoonshineButton, Icon } from "@speakeasy-api/moonshine";

const RULE_ID_TO_CATEGORY = new Map<string, RuleCategory>();
for (const [category, rules] of Object.entries(DETECTION_RULES)) {
  for (const rule of rules) {
    RULE_ID_TO_CATEGORY.set(rule.id, category as RuleCategory);
  }
}

const SOURCE_TO_CATEGORY = new Map<string, RuleCategory>([
  ["destructive_tool", "destructive_tool"],
  ["shadow_mcp", "shadow_mcp"],
  ["prompt_injection", "prompt_injection"],
]);

function getCategoryForFinding(
  source: string | undefined,
  ruleId: string | undefined,
): RuleCategory | null {
  const sourceCategory = source ? SOURCE_TO_CATEGORY.get(source) : null;
  if (sourceCategory) return sourceCategory;
  if (!ruleId) return null;
  return RULE_ID_TO_CATEGORY.get(ruleId) ?? null;
}

function CategoryLabel({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}) {
  const category = getCategoryForFinding(source, ruleId);
  const label = category ? RULE_CATEGORY_META[category].label : null;
  return <span className="font-mono text-xs">{label}</span>;
}

export default function SecurityOverview() {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <SecurityOverviewContent />
        </Page.Body>
      </Page>
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
  const recentChats = useMemo(
    () => chatSummaryQuery.data?.pages.flatMap((p) => p.chats) ?? [],
    [chatSummaryQuery.data],
  );

  const isInitialLoading =
    policiesLoading || resultsQuery.isLoading || chatSummaryQuery.isLoading;

  if (isInitialLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <p className="text-muted-foreground text-sm">Loading...</p>
      </div>
    );
  }

  if (policies.length === 0) {
    return (
      <Page.Section>
        <Page.Section.Title>Risk Overview</Page.Section.Title>
        <Page.Section.Description className="max-w-2xl">
          Recent findings from risk analysis scans across your project.
        </Page.Section.Description>
        <Page.Section.CTA>
          <MoonshineButton
            variant="secondary"
            onClick={() => routes.policyCenter.goTo()}
          >
            <MoonshineButton.Text>Manage Policies</MoonshineButton.Text>
            <MoonshineButton.RightIcon>
              <Icon name="arrow-right" />
            </MoonshineButton.RightIcon>
          </MoonshineButton>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="flex flex-col items-center justify-center gap-4 py-20">
            <Shield className="text-muted-foreground h-12 w-12" />
            <h2 className="text-lg font-semibold">Risk Analysis</h2>
            <p className="text-muted-foreground max-w-md text-center text-sm">
              Monitor your chat messages for leaked secrets and sensitive data.
              Set up a risk policy to get started.
            </p>
          </div>
        </Page.Section.Body>
      </Page.Section>
    );
  }

  const totalScanned = policies.reduce(
    (max, p) => Math.max(max, p.totalMessages - p.pendingMessages),
    0,
  );
  const totalFindings =
    resultsQuery.data?.pages[0]?.totalCount ?? results.length;

  const hasData = recentChats.length > 0 || results.length > 0;

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Risk Overview</Page.Section.Title>

        <Page.Section.Description>
          Recent findings from risk analysis scans across your project.
        </Page.Section.Description>

        <Page.Section.CTA>
          <MoonshineButton
            variant="secondary"
            onClick={() => routes.policyCenter.goTo()}
          >
            <MoonshineButton.Text>Manage Policies</MoonshineButton.Text>
            <MoonshineButton.RightIcon>
              <Icon name="arrow-right" />
            </MoonshineButton.RightIcon>
          </MoonshineButton>
        </Page.Section.CTA>

        <Page.Section.Body>
          <div className="mt-4 grid grid-cols-2 gap-4">
            <MetricCard
              title="Events Scanned"
              value={totalScanned}
              format="number"
              icon="scan-search"
            />
            <MetricCard
              title="Recent Findings"
              value={totalFindings}
              format="number"
              icon="flag"
            />
          </div>
        </Page.Section.Body>
      </Page.Section>

      {hasData ? (
        <>
          {recentChats.length > 0 && (
            <Page.Section>
              <Page.Section.Title>Recent Chats</Page.Section.Title>
              <Page.Section.Body>
                <div className="max-h-[412px] overflow-auto rounded-md border **:data-[slot=table-container]:overflow-visible">
                  <Table>
                    <TableHeader className="bg-background sticky top-0 z-10">
                      <TableRow>
                        <TableHead className="w-6/12 pl-4">Chat</TableHead>
                        <TableHead className="w-3/12">User</TableHead>
                        <TableHead className="w-1/12">Findings</TableHead>
                        <TableHead className="w-2/12 pr-4">
                          Latest Detected
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {recentChats.map((chat) => (
                        <TableRow
                          key={chat.chatId}
                          className="cursor-pointer"
                          onClick={() => setSelectedChatId(chat.chatId)}
                        >
                          <TableCell className="text-muted-foreground truncate pl-4">
                            {chat.chatTitle ?? "Untitled"}
                          </TableCell>
                          <TableCell className="text-muted-foreground">
                            {chat.userId ?? "-"}
                          </TableCell>
                          <TableCell className="text-foreground font-mono">
                            {chat.findingsCount}
                          </TableCell>
                          <TableCell className="text-muted-foreground pr-4">
                            {chat.latestDetected
                              ? new Date(chat.latestDetected).toLocaleString()
                              : "-"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
                {chatSummaryQuery.hasNextPage && (
                  <div className="flex justify-center">
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
              </Page.Section.Body>
            </Page.Section>
          )}

          {results.length > 0 && (
            <Page.Section>
              <Page.Section.Title>Recent Findings</Page.Section.Title>
              <Page.Section.Body>
                <div className="max-h-[412px] overflow-auto rounded-md border **:data-[slot=table-container]:overflow-visible">
                  <Table>
                    <TableHeader className="bg-background sticky top-0 z-10">
                      <TableRow>
                        <TableHead className="w-1/12 pl-4">Category</TableHead>
                        <TableHead className="w-1/12">Rule</TableHead>
                        <TableHead className="w-1/12">Chat</TableHead>
                        <TableHead className="w-1/12">User</TableHead>
                        <TableHead className="w-1/12">Match</TableHead>
                        <TableHead className="w-1/12">Policy Note</TableHead>
                        <TableHead className="w-1/12 pr-4">Detected</TableHead>
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
                            <TableCell className="pl-4">
                              <CategoryLabel
                                source={result.source}
                                ruleId={result.ruleId}
                              />
                            </TableCell>
                            <TableCell>
                              <span className="font-mono text-xs">
                                {result.ruleId ? result.ruleId : "-"}
                              </span>
                            </TableCell>
                            <TableCell className="text-muted-foreground truncate">
                              {result.chatTitle ?? "Untitled"}
                            </TableCell>
                            <TableCell className="text-muted-foreground">
                              {result.userId ?? "-"}
                            </TableCell>
                            <TableCell className="truncate">
                              <MaskedMatch value={result.match} />
                            </TableCell>
                            <TableCell
                              className="text-muted-foreground truncate italic"
                              title={policyNote ?? undefined}
                            >
                              {policyNote ?? "-"}
                            </TableCell>
                            <TableCell className="text-muted-foreground pr-4">
                              {result.createdAt
                                ? new Date(result.createdAt).toLocaleString()
                                : "-"}
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </div>
                {resultsQuery.hasNextPage && (
                  <div className="flex justify-center">
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
              </Page.Section.Body>
            </Page.Section>
          )}
        </>
      ) : (
        <div className="mt-8 text-center">
          <p className="text-muted-foreground text-sm">
            No findings yet. Findings will appear here as messages are analyzed.
          </p>
        </div>
      )}

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
    </>
  );
}
