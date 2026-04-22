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
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";
import { keepPreviousData } from "@tanstack/react-query";
import {
  useRiskListResults,
  useRiskListPolicies,
  useRiskListResultsByChat,
} from "@gram/client/react-query/index.js";
import { RULE_CATEGORY_META } from "./policy-data";
import { ChatDetailPanel } from "@/pages/chatLogs/ChatDetailPanel";
import { Drawer, DrawerContent } from "@/components/ui/drawer";

export default function SecurityOverview() {
  return (
    <RequireScope scope="org:admin" level="page">
      <SecurityOverviewContent />
    </RequireScope>
  );
}

function SecurityOverviewContent() {
  const routes = useRoutes();
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
  const [chatsLimit, setChatsLimit] = useState(10);
  const [findingsLimit, setFindingsLimit] = useState(10);

  const { data: policiesData, isLoading: policiesLoading } =
    useRiskListPolicies();
  const {
    data: resultsData,
    isLoading: resultsLoading,
    isFetching: resultsFetching,
  } = useRiskListResults({ limit: findingsLimit }, undefined, {
    placeholderData: keepPreviousData,
  });
  const {
    data: chatSummaryData,
    isLoading: chatSummaryLoading,
    isFetching: chatSummaryFetching,
  } = useRiskListResultsByChat({ limit: chatsLimit }, undefined, {
    placeholderData: keepPreviousData,
  });

  const policies = policiesData?.policies ?? [];
  const results = resultsData?.results ?? [];
  const recentChats = chatSummaryData?.chats ?? [];

  // Only show full-page loading on initial load, not on limit changes
  const isInitialLoading =
    policiesLoading || resultsLoading || chatSummaryLoading;

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
          <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
            <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
              <Shield className="text-muted-foreground h-6 w-6" />
            </div>
            <Type variant="subheading" className="mb-1">
              Risk Analysis
            </Type>
            <Type small muted className="mb-4 max-w-md text-center">
              Monitor your chat messages for leaked secrets and sensitive data.
              Set up a risk policy to get started.
            </Type>
            <Button onClick={() => routes.policyCenter.goTo()}>
              Go to Policy Center
            </Button>
          </div>
        </Page.Body>
      </Page>
    );
  }

  const totalFindings = resultsData?.totalCount ?? 0;
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
                  {recentChats.length >= chatsLimit && (
                    <div className="mt-2 flex justify-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={chatSummaryFetching}
                        onClick={() => setChatsLimit((l) => l + 100)}
                      >
                        {chatSummaryFetching ? "Loading..." : "Load More"}
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
                        <TableHead>Match</TableHead>
                        <TableHead>Detected</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {results.map((result) => (
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
                            <Badge variant="secondary">
                              {RULE_CATEGORY_META.secrets.label}
                            </Badge>
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
                          <TableCell className="max-w-xs truncate font-mono text-xs">
                            {result.match
                              ? result.match.length > 40
                                ? `${result.match.slice(0, 20)}...${result.match.slice(-10)}`
                                : result.match
                              : "-"}
                          </TableCell>
                          <TableCell className="text-muted-foreground text-xs">
                            {result.createdAt
                              ? new Date(result.createdAt).toLocaleString()
                              : "-"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {results.length >= findingsLimit && (
                    <div className="mt-2 flex justify-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={resultsFetching}
                        onClick={() => setFindingsLimit((l) => l + 100)}
                      >
                        {resultsFetching ? "Loading..." : "Load More"}
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
