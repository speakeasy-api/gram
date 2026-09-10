import {
  DeleteNetworkIngressDialog,
  RotateNetworkIngressCredentialsDialog,
} from "./PrivateNetworkLifecycleDialogs";
import {
  invalidateAllNetworkIngress,
  useNetworkIngress,
} from "@gram/client/react-query/networkIngress.js";

import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { HumanizeDateTime } from "@/lib/dates";
import { InlineEmptyState } from "@/components/inline-empty-state";
import type { NetworkIngress } from "@gram/client/models/components/networkingress.js";
import { PrivateNetworkSetupSheet } from "./PrivateNetworkSetupSheet";
import { RequireScope } from "@/components/require-scope";
import { SettingsSection } from "@/components/page-templates";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { toast } from "sonner";
import { useNetworkIngressCheckHealthMutation } from "@gram/client/react-query/networkIngressCheckHealth.js";
import { useNetworkIngressRollout } from "@/hooks/useNetworkIngressRollout";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useUpdateNetworkIngressMutation } from "@gram/client/react-query/updateNetworkIngress.js";

function statusVariant(
  status: string,
): "neutral" | "success" | "warning" | "destructive" {
  switch (status) {
    case "online":
      return "success";
    case "pending":
    case "degraded":
      return "warning";
    case "error":
      return "destructive";
    default:
      return "neutral";
  }
}

function statusLabel(status: string): string {
  return status.replaceAll("_", " ");
}

function ConfiguredPrivateNetwork({
  ingress,
  entitled,
}: {
  ingress: NetworkIngress;
  entitled: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [rotateOpen, setRotateOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const update = useUpdateNetworkIngressMutation({
    onSuccess: async () => {
      await invalidateAllNetworkIngress(queryClient);
      toast.success("Private network settings updated");
    },
    onError: (error) =>
      handleAPIError(error, "Failed to update private network"),
  });
  const health = useNetworkIngressCheckHealthMutation({
    onSuccess: async () => {
      await invalidateAllNetworkIngress(queryClient);
      toast.success("Private network health check requested");
    },
    onError: (error) =>
      handleAPIError(error, "Failed to check private network health"),
  });

  return (
    <SettingsSection.Panel>
      <SettingsSection.Body>
        {!entitled && (
          <Alert variant="warning" dismissible={false}>
            Private network access is no longer enabled for this organization.
            Existing restrictions remain enforced. You can disable or remove
            this ingress, and restore affected MCP servers to public-only
            access.
          </Alert>
        )}
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Text variant="subheading">Tailscale</Text>
              <Badge variant={statusVariant(ingress.status)} background>
                {statusLabel(ingress.status)}
              </Badge>
            </div>
            <Text small muted>
              Hostname label <code>{ingress.hostname}</code>
            </Text>
          </div>
          <div className="text-right">
            {ingress.healthCheckedAt && (
              <Text small muted>
                Last checked <HumanizeDateTime date={ingress.healthCheckedAt} />
              </Text>
            )}
            {ingress.connectedSince && (
              <Text small muted>
                Connected <HumanizeDateTime date={ingress.connectedSince} />
              </Text>
            )}
          </div>
        </div>
        <dl className="grid gap-4 border-t pt-4 sm:grid-cols-3">
          <div>
            <dt className="text-eyebrow">Private base URL</dt>
            <dd className="mt-1 break-all font-mono text-sm">
              {ingress.dnsName
                ? `https://${ingress.dnsName}`
                : "Waiting for Tailscale"}
            </dd>
          </div>
          <div>
            <dt className="text-eyebrow">Endpoint namespace</dt>
            <dd className="mt-1 text-sm">
              {ingress.endpointNamespaceKind === "custom_domain"
                ? "Custom domain"
                : "Gram platform"}
            </dd>
          </div>
          <div>
            <dt className="text-eyebrow">Credentials</dt>
            <dd className="mt-1 text-sm">
              {ingress.credentialsConfigured ? "Configured" : "Not configured"}
            </dd>
          </div>
        </dl>
        {ingress.lastError && (
          <Alert variant="error" dismissible={false}>
            Latest check: {statusLabel(ingress.lastError)}
          </Alert>
        )}
        <div className="flex items-start justify-between gap-6 border-t pt-4">
          <div className="space-y-1">
            <Text variant="subheading">Require user identity</Text>
            <Text small muted>
              Tagged devices and service nodes are denied when this is enabled.
            </Text>
          </div>
          <Switch
            aria-label="Require user identity"
            checked={ingress.identityRequired}
            disabled={!entitled || update.isPending}
            onCheckedChange={(identityRequired) =>
              update.mutate({
                security: { sessionHeaderGramSession: "" },
                request: { updateIngressRequestBody: { identityRequired } },
              })
            }
          />
        </div>
        <div className="flex items-start justify-between gap-6 border-t pt-4">
          <div className="space-y-1">
            <Text variant="subheading">Ingress enabled</Text>
            <Text small muted>
              Disabling stops private serving without changing any MCP server
              network mode.
            </Text>
          </div>
          <Switch
            aria-label="Private network ingress enabled"
            checked={ingress.enabled}
            disabled={update.isPending || (!entitled && !ingress.enabled)}
            onCheckedChange={(enabled) =>
              update.mutate({
                security: { sessionHeaderGramSession: "" },
                request: { updateIngressRequestBody: { enabled } },
              })
            }
          />
        </div>
      </SettingsSection.Body>
      <SettingsSection.Footer>
        <SettingsSection.FooterHint>
          There is no automatic public fallback when the private route is
          unavailable.
        </SettingsSection.FooterHint>
        <SettingsSection.FooterActions>
          <Button
            variant="secondary"
            size="sm"
            disabled={!entitled || health.isPending}
            onClick={() =>
              health.mutate({ security: { sessionHeaderGramSession: "" } })
            }
          >
            {health.isPending ? "Checking..." : "Check health"}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            disabled={!entitled}
            onClick={() => setRotateOpen(true)}
          >
            Rotate credentials
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            className="hover:text-destructive"
            onClick={() => setDeleteOpen(true)}
          >
            Remove
          </Button>
        </SettingsSection.FooterActions>
      </SettingsSection.Footer>
      <RotateNetworkIngressCredentialsDialog
        open={rotateOpen}
        onOpenChange={setRotateOpen}
      />
      <DeleteNetworkIngressDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        ingress={ingress}
      />
    </SettingsSection.Panel>
  );
}

export function PrivateNetworkSection(): JSX.Element | null {
  const organization = useOrganization();
  const { adminRolloutEnabled: rolloutEnabled } = useNetworkIngressRollout();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { enabled: rolloutEnabled, throwOnError: false },
  );
  const entitled = features.data?.networkIngressEnabled === true;
  const ingressResult = useNetworkIngress(undefined, undefined, {
    enabled: rolloutEnabled,
    retry: (failureCount) => failureCount < 2,
    throwOnError: false,
  });
  const ingress = ingressResult.data?.ingress;
  const [setupOpen, setSetupOpen] = useState(false);

  if (!rolloutEnabled) return null;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Private network access</SettingsSection.Title>
        <SettingsSection.Description>
          Serve hosted MCP endpoints inside your Tailscale network without
          exposing a public fallback.
        </SettingsSection.Description>
      </SettingsSection.Header>
      {ingressResult.isLoading || features.isLoading ? (
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <Text small muted>
              Loading private network settings...
            </Text>
          </SettingsSection.Body>
        </SettingsSection.Panel>
      ) : ingressResult.isError ? (
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <Alert variant="error" dismissible={false}>
              Private network settings could not be loaded. No controls are
              available until the request succeeds.
            </Alert>
          </SettingsSection.Body>
        </SettingsSection.Panel>
      ) : ingress ? (
        <ConfiguredPrivateNetwork ingress={ingress} entitled={entitled} />
      ) : (
        <InlineEmptyState
          icon="network"
          heading="No private network connected"
          description={
            entitled
              ? "Connect a Tailscale tailnet to create private URLs for this organization."
              : "Private network access is not enabled for this organization."
          }
          action={
            entitled ? (
              <RequireScope scope="org:admin" level="component">
                <Button size="sm" onClick={() => setSetupOpen(true)}>
                  Connect Tailscale
                </Button>
              </RequireScope>
            ) : undefined
          }
        />
      )}
      <PrivateNetworkSetupSheet open={setupOpen} onOpenChange={setSetupOpen} />
    </SettingsSection>
  );
}
