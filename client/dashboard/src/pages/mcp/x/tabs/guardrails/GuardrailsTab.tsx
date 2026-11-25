import { InlineEmptyState } from "@/components/inline-empty-state";
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
  serverGuardrailPolicies,
} from "@/pages/security/server-guardrails/server-policies";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
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
  require_approval: "Require approval",
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
        <Stack gap={6}>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Text small muted>
              Guardrails check traffic through this server for secrets,
              sensitive data and risky tool calls.
            </Text>
            <SegmentedControl<GuardrailsView>
              value={view}
              onChange={setView}
              options={[
                { value: "policies", label: "Guardrails" },
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
  const applicableQuery = useRiskListPoliciesForMcpServer(
    { mcpServerId: mcpServer.id },
    undefined,
    { throwOnError: false },
  );
  // The server-scoped endpoint returns enabled policies only; the full list
  // supplies the disabled ones so they stay visible and can be re-enabled.
  const allQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const policiesQuery = {
    isLoading: applicableQuery.isLoading || allQuery.isLoading,
    isError: applicableQuery.isError || allQuery.isError,
    error: applicableQuery.error ?? allQuery.error,
  };
  const [adding, setAdding] = useState(false);
  const { scoped, inherited } = useMemo(
    () =>
      policiesForMcpServer(
        serverGuardrailPolicies(
          applicableQuery.data?.policies ?? [],
          allQuery.data?.policies ?? [],
          mcpServer.id,
        ),
        mcpServer.id,
      ),
    [applicableQuery.data?.policies, allQuery.data?.policies, mcpServer.id],
  );

  return (
    <Stack gap={6}>
      <section className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-eyebrow">This server</h3>
          {/* The empty state carries the add action, so only one shows. */}
          {scoped.length > 0 && (
            <Button size="sm" onClick={() => setAdding(true)}>
              <Button.Text>Add guardrail</Button.Text>
            </Button>
          )}
        </div>
        {policiesQuery.isLoading ? (
          <Text small muted>
            Loading guardrails…
          </Text>
        ) : policiesQuery.isError ? (
          <div role="alert">
            <InlineEmptyState
              icon="triangle-alert"
              heading="Couldn't load guardrails"
              description="Check your connection, then try again."
              action={
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    void applicableQuery.refetch();
                    void allQuery.refetch();
                  }}
                >
                  <Button.Text>Retry</Button.Text>
                </Button>
              }
            />
          </div>
        ) : scoped.length === 0 ? (
          <InlineEmptyState
            icon="shield-check"
            heading="Protect this server's traffic"
            description="Add a guardrail to catch secrets, sensitive data and risky tool calls on this server."
            action={
              <Button size="sm" onClick={() => setAdding(true)}>
                <Button.Text>Add guardrail</Button.Text>
              </Button>
            }
          />
        ) : (
          <PolicyRows
            policies={scoped}
            detail={(policy) => scopedToolsLabel(policy, mcpServer.id)}
          />
        )}
      </section>
      <section className="space-y-3">
        <div>
          <h3 className="text-eyebrow">All servers</h3>
          <Text small muted>
            Guardrails that apply to every MCP server. Manage them under{" "}
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
            No guardrails apply to every server yet.
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
