import { Page } from "@/components/page-layout";
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
import { Shield, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useNavigate } from "react-router";
import {
  useRiskListResults,
  useRiskListPolicies,
} from "@gram/client/react-query/index.js";
import { RULE_CATEGORY_META } from "./policy-data";

export default function DlpOverview() {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data: policiesData, isLoading: policiesLoading } =
    useRiskListPolicies();
  const { data: resultsData, isLoading: resultsLoading } = useRiskListResults({
    limit: 50,
  });

  const policies = policiesData?.policies ?? [];
  const results = resultsData?.results ?? [];

  const isLoading = policiesLoading || resultsLoading;

  if (isLoading) {
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

  const totalFindings = results.length;
  const totalScanned = policies.reduce(
    (max, p) => Math.max(max, p.totalMessages - p.pendingMessages),
    0,
  );

  return (
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
          <Button variant="outline" onClick={() => routes.policyCenter.goTo()}>
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
            <p className="text-muted-foreground text-sm">Total Findings</p>
            <p className="text-2xl font-bold">
              {totalFindings.toLocaleString()}
            </p>
          </div>
        </div>

        {results.length > 0 ? (
          <div className="mt-6">
            <h3 className="mb-2 text-sm font-semibold">Recent Findings</h3>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Category</TableHead>
                  <TableHead>Rule</TableHead>
                  <TableHead>Match</TableHead>
                  <TableHead>Detected</TableHead>
                  <TableHead className="w-[100px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {results.map((result) => (
                  <TableRow
                    key={result.id}
                    className="cursor-pointer"
                    onClick={() => {
                      if (result.chatId) {
                        const params = new URLSearchParams({
                          chatId: result.chatId,
                          search: result.chatId,
                          range: "90d",
                        });
                        navigate(
                          `${routes.chatSessions.href()}?${params.toString()}`,
                        );
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
                    <TableCell>
                      {result.chatId && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-muted-foreground hover:text-foreground gap-1 text-xs"
                          onClick={(e) => {
                            e.stopPropagation();
                            const params = new URLSearchParams({
                              chatId: result.chatId!,
                              search: result.chatId!,
                              range: "90d",
                            });
                            navigate(
                              `${routes.chatSessions.href()}?${params.toString()}`,
                            );
                          }}
                        >
                          View Chat
                          <ExternalLink className="h-3 w-3" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
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
  );
}
