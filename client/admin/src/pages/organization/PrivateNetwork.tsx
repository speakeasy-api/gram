import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import type { JSX } from "react";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { organizationQuery } from "@/lib/adminQueries";
import {
  moveOrganizationNetworkIngressToGateway,
  organizationNetworkIngressQuery,
} from "@/lib/gramAdminClient";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import { useWriteReport } from "@/pages/organizations/writeReport";
import type { AdminNetworkIngress } from "@gram/admin-client/models/components/adminnetworkingress";
import type { AdminNetworkIngressGatewayNode } from "@gram/admin-client/models/components/adminnetworkingressgatewaynode";

// While a move settles, poll so the page shows the gateway nodes coming up
// without a manual refresh. Once the ingress is online it stops.
const SETTLING_POLL_MS = 5_000;

export function PrivateNetworkRoute(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return <p className="text-muted-foreground text-sm">Loading…</p>;
  return <PrivateNetwork key={data.id} org={data} />;
}

export function PrivateNetwork({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const { announce, showFailure } = useWriteReport();
  const query = organizationNetworkIngressQuery(org.id);
  const { data, error, isPending } = useQuery({
    ...query,
    refetchInterval: (q) => {
      const ingress = q.state.data?.ingress;
      return ingress?.runtime === "gateway" && ingress.status !== "online"
        ? SETTLING_POLL_MS
        : false;
    },
  });
  const move = useMutation({
    mutationFn: () => moveOrganizationNetworkIngressToGateway(org.id),
    onSuccess: (updated) => {
      qc.setQueryData(query.queryKey, updated);
    },
  });

  if (isPending) {
    return <p className="text-muted-foreground text-sm">Loading…</p>;
  }
  if (error) {
    return (
      <p className="text-destructive text-sm">
        Could not load the private network: {errorMessage(error)}
      </p>
    );
  }
  const ingress = data.ingress;

  const onMove = async (current: AdminNetworkIngress) => {
    const confirmed = await confirm({
      title: `Move ${org.name} to the tailnet gateway?`,
      description:
        "Private MCP access for this organization will be unavailable for a few minutes while the Tailscale Service changes hosts. Its address does not change.",
      details: (
        <div className="space-y-3 text-sm">
          <p>Before moving, check the customer has:</p>
          <ul className="list-disc space-y-1 pl-5">
            <li>
              Given their saved OAuth client the <code>{data.gatewayTag}</code>{" "}
              tag, with Auth Keys, Devices Core and Services write scopes.
              Keeping its old tag as well is fine.
            </li>
            <li>
              Let <code>{data.gatewayTag}</code> host{" "}
              <code>svc:{current.hostname}</code> in their policy&apos;s{" "}
              <code>autoApprovers.services</code>.
            </li>
          </ul>
          <p className="text-muted-foreground">
            The saved client is checked first by minting and revoking a{" "}
            <code>{data.gatewayTag}</code> key. If that fails, nothing changes.
          </p>
        </div>
      ),
      confirmLabel: "Move to tailnet gateway",
      destructive: true,
    });
    if (!confirmed) return;

    showFailure(null);
    try {
      await move.mutateAsync();
      announce(
        `Moving ${org.name}'s private network to the tailnet gateway. The operator resources are removed first.`,
      );
    } catch (err) {
      const text = `Could not move ${org.name} to the tailnet gateway: ${errorMessage(err)}`;
      announce(text);
      showFailure(text);
    }
  };

  return (
    <div className="space-y-6">
      {confirmDialog}
      <section className="border-border overflow-hidden rounded-md border">
        <header className="bg-muted/50 flex items-center justify-between gap-4 px-4 py-3">
          <div>
            <h2 className="text-sm font-medium">Private network</h2>
            <p className="text-muted-foreground text-xs">
              The organization&apos;s Tailscale connection and what hosts it.
            </p>
          </div>
          {ingress?.runtime === "operator" && (
            <Button
              size="sm"
              disabled={move.isPending}
              onClick={() => void onMove(ingress)}
            >
              {move.isPending
                ? "Checking credentials…"
                : "Move to tailnet gateway"}
            </Button>
          )}
        </header>
        {ingress ? (
          <IngressDetails ingress={ingress} />
        ) : (
          <p className="text-muted-foreground px-4 py-3 text-sm">
            This organization has not connected a tailnet. When it does, it
            starts on the tailnet gateway.
          </p>
        )}
      </section>
      {ingress?.runtime === "gateway" && (
        <GatewayNodes nodes={ingress.gatewayNodes} />
      )}
    </div>
  );
}

function IngressDetails({
  ingress,
}: {
  ingress: AdminNetworkIngress;
}): JSX.Element {
  return (
    <dl className="divide-border divide-y text-sm">
      <Row label="Hosted by">
        <Badge
          variant={ingress.runtime === "gateway" ? "default" : "secondary"}
        >
          {ingress.runtime === "gateway"
            ? "Tailnet gateway"
            : "Tailscale operator"}
        </Badge>
      </Row>
      <Row label="Status">
        <span className="flex items-center gap-2">
          <Badge variant={statusVariant(ingress.status)}>
            {ingress.status}
          </Badge>
          {ingress.lastError && (
            <code className="text-muted-foreground text-xs">
              {ingress.lastError}
            </code>
          )}
        </span>
      </Row>
      <Row label="Address">
        <code>{ingress.dnsName ?? `${ingress.hostname}.<tailnet>.ts.net`}</code>
      </Row>
      <Row label="Turned on">{ingress.enabled ? "Yes" : "No"}</Row>
      <Row label="Credentials saved">
        {ingress.credentialsConfigured ? "Yes" : "No"}
      </Row>
      <Row label="Last checked">{formatTime(ingress.healthCheckedAt)}</Row>
      <Row label="Online since">{formatTime(ingress.connectedSince)}</Row>
    </dl>
  );
}

function GatewayNodes({
  nodes,
}: {
  nodes: AdminNetworkIngressGatewayNode[];
}): JSX.Element {
  return (
    <section className="border-border overflow-hidden rounded-md border">
      <header className="bg-muted/50 px-4 py-3">
        <h2 className="text-sm font-medium">Gateway nodes</h2>
        <p className="text-muted-foreground text-xs">
          One tailnet device per gateway pod. They appear after the reconciler
          has removed the operator resources.
        </p>
      </header>
      {nodes.length === 0 ? (
        <p className="text-muted-foreground px-4 py-3 text-sm">
          No gateway nodes yet.
        </p>
      ) : (
        <table className="w-full text-sm">
          <thead className="text-muted-foreground text-left text-xs">
            <tr className="border-border border-b">
              <th className="px-4 py-2 font-normal">Slot</th>
              <th className="px-4 py-2 font-normal">Status</th>
              <th className="px-4 py-2 font-normal">Pod</th>
              <th className="px-4 py-2 font-normal">Last seen</th>
              <th className="px-4 py-2 font-normal">Problem</th>
            </tr>
          </thead>
          <tbody className="divide-border divide-y">
            {nodes.map((node) => (
              <tr key={node.slot}>
                <td className="px-4 py-2">{node.slot}</td>
                <td className="px-4 py-2">
                  <Badge
                    variant={
                      node.retired ? "outline" : statusVariant(node.status)
                    }
                  >
                    {node.retired ? "retiring" : node.status}
                  </Badge>
                </td>
                <td className="px-4 py-2">
                  <code className="text-xs">{node.podName ?? "—"}</code>
                </td>
                <td className="px-4 py-2">{formatTime(node.lastSeenAt)}</td>
                <td
                  className="text-muted-foreground max-w-xs truncate px-4 py-2"
                  title={node.lastError}
                >
                  {node.lastError ?? "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[10rem_1fr] items-center gap-4 px-4 py-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

function statusVariant(
  status: string,
): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "online":
      return "default";
    case "error":
    case "degraded":
      return "destructive";
    default:
      return "secondary";
  }
}

function formatTime(value: Date | undefined): string {
  return value ? value.toLocaleString() : "—";
}
