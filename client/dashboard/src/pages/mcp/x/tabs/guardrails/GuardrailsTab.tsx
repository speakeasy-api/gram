import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { policyStatusLabel } from "@/pages/security/policy-enabled";
import { SeverityBadge } from "@/pages/security/risk-ui";
import {
  policiesForMcpServer,
  scopedToolsLabel,
} from "@/pages/security/server-guardrails/server-policies";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useRiskListPoliciesForMcpServer } from "@gram/client/react-query/riskListPoliciesForMcpServer.js";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { AddServerGuardrailDialog } from "./AddServerGuardrailDialog";
import { ServerRiskEvents } from "./ServerRiskEvents";

type GuardrailsView = "policies" | "events";

const VIEW_PARAM = "view";

const ACTION_LABELS: Record<RiskPolicy["action"], string> = {
  flag: "Log",
  warn: "Warn & confirm",
  block: "Deny",
  quarantine: "Quarantine",
};

/** Guardrails for one MCP server: the policies scoped to it (plus the ones it
 *  inherits) and the risk events raised on it. */
export function GuardrailsTab({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const organization = useOrganization();
  const [searchParams, setSearchParams] = useSearchParams();
  const view: GuardrailsView =
    searchParams.get(VIEW_PARAM) === "events" ? "events" : "policies";

  const setView = (next: GuardrailsView) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (next === "policies") params.delete(VIEW_PARAM);
        else params.set(VIEW_PARAM, next);
        return params;
      },
      { replace: true },
    );

  return (
    <RequireScope scope="org:admin" resourceId={organization.id} level="page">
      <RequireScope scope="mcp:read" resourceId={mcpServer.id} level="page">
        <Stack gap={6} className="py-8">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="font-display text-3xl font-thin">Guardrails</h2>
              <Text small muted>
                Risk policies that inspect traffic through this server.
              </Text>
            </div>
            <SegmentedControl<GuardrailsView>
              value={view}
              onChange={setView}
              options={[
                { value: "policies", label: "Policies" },
                { value: "events", label: "Risk events" },
              ]}
            />
          </div>
          {view === "policies" ? (
            <PoliciesView mcpServer={mcpServer} />
          ) : (
            <ServerRiskEvents mcpServer={mcpServer} />
          )}
        </Stack>
      </RequireScope>
    </RequireScope>
  );
}

function PoliciesView({ mcpServer }: { mcpServer: McpServer }): JSX.Element {
  const routes = useRoutes();
  // The server resolves what applies here, including gateway membership, so
  // the tab never re-derives scope rules on the client.
  const policiesQuery = useRiskListPoliciesForMcpServer(
    { mcpServerId: mcpServer.id },
    undefined,
    { throwOnError: false },
  );
  const [adding, setAdding] = useState(false);
  const { scoped, inherited } = useMemo(
    () =>
      policiesForMcpServer(policiesQuery.data?.policies ?? [], mcpServer.id),
    [policiesQuery.data?.policies, mcpServer.id],
  );

  return (
    <Stack gap={6}>
      <section className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-eyebrow">Scoped to this server</h3>
          <Button size="sm" onClick={() => setAdding(true)}>
            <Button.Text>Add guardrail</Button.Text>
          </Button>
        </div>
        {policiesQuery.isLoading ? (
          <Text small muted>
            Loading policies…
          </Text>
        ) : policiesQuery.isError ? (
          <Text small className="text-destructive" role="alert">
            {policiesQuery.error.message}
          </Text>
        ) : scoped.length === 0 ? (
          <div className="border-border border border-dashed p-6 text-center">
            <Text small muted>
              No guardrail is scoped to this server yet. Add one to inspect its
              traffic for secrets, sensitive data and risky tool calls.
            </Text>
          </div>
        ) : (
          <PolicyRows
            policies={scoped}
            detail={(policy) => scopedToolsLabel(policy, mcpServer.id)}
          />
        )}
      </section>
      <section className="space-y-3">
        <div>
          <h3 className="text-eyebrow">Inherited</h3>
          <Text small muted>
            Policies scoped to every MCP server. Manage them under{" "}
            <Link
              to={routes.policyCenter.href()}
              className="underline underline-offset-2"
            >
              Guardrails
            </Link>
            .
          </Text>
        </div>
        {policiesQuery.isError ? null : inherited.length === 0 ? (
          <Text small muted>
            No all-servers policies apply to this server.
          </Text>
        ) : (
          <PolicyRows policies={inherited} detail={() => "All servers"} />
        )}
      </section>
      <AddServerGuardrailDialog
        mcpServer={mcpServer}
        open={adding}
        onOpenChange={setAdding}
      />
    </Stack>
  );
}

function PolicyRows({
  policies,
  detail,
}: {
  policies: RiskPolicy[];
  detail: (policy: RiskPolicy) => string;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <div className="border-border divide-border divide-y border">
      {policies.map((policy) => (
        <Link
          key={policy.id}
          to={routes.policyCenter.detail.href(policy.id)}
          className="hover:bg-muted/40 flex flex-wrap items-center gap-x-6 gap-y-1 px-4 py-3"
        >
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{policy.name}</div>
            <div className="text-muted-foreground font-mono text-xs">
              {detail(policy)}
            </div>
          </div>
          <span className="text-muted-foreground font-mono text-xs">
            {ACTION_LABELS[policy.action]}
          </span>
          <SeverityBadge score={policy.score} />
          <span className="text-muted-foreground font-mono text-xs uppercase">
            {policyStatusLabel(policy.enabled)}
          </span>
        </Link>
      ))}
    </div>
  );
}
