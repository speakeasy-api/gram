import { RequireScope } from "@/components/require-scope";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useNetworkIngressRollout } from "@/hooks/useNetworkIngressRollout";
import { customDomainMcpEndpointUrl } from "@/hooks/useToolsetUrl";

import { getServerURL } from "@/lib/utils";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import {
  McpServerNetworkAccessMode,
  type McpServer,
} from "@gram/client/models/components/mcpserver.js";
import type { UpdateMcpServerFormNetworkAccessMode } from "@gram/client/models/components/updatemcpserverform.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import { useListDomains } from "@gram/client/react-query/listDomains.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

const NETWORK_ACCESS_LABELS: Record<
  UpdateMcpServerFormNetworkAccessMode,
  string
> = {
  public_only: "Public only",
  dual: "Public and private",
  private_only: "Private only",
};

export function NetworkAccessSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}): JSX.Element | null {
  const { rolloutEnabled, canManageIngress } = useNetworkIngressRollout();

  if (!rolloutEnabled) {
    return null;
  }

  return (
    <NetworkAccessSectionContent
      mcpServer={mcpServer}
      endpoints={endpoints}
      canReadIngress={canManageIngress}
    />
  );
}

function NetworkAccessSectionContent({
  mcpServer,
  endpoints,
  canReadIngress,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  canReadIngress: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<UpdateMcpServerFormNetworkAccessMode>(
    mcpServer.networkAccessMode,
  );
  const [confirmPrivateOnlyOpen, setConfirmPrivateOnlyOpen] = useState(false);

  useEffect(() => {
    setDraft(mcpServer.networkAccessMode);
  }, [mcpServer.id, mcpServer.networkAccessMode]);

  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const ingressResult = useNetworkIngress(undefined, undefined, {
    enabled: canReadIngress,
    retry: false,
    throwOnError: false,
  });
  const ingress = ingressResult.data?.ingress;
  const hasCustomDomainEndpoints = endpoints.some(
    (endpoint) => endpoint.customDomainId,
  );
  const domainsResult = useListDomains(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
    enabled: hasCustomDomainEndpoints,
  });
  const domains = domainsResult.data?.domains;

  const entitled = features.data?.networkIngressEnabled === true;
  const ingressOnline =
    ingress?.enabled === true && ingress.status === "online";
  const eligibleEndpoints = useMemo(
    () =>
      endpoints.filter((endpoint) => {
        if (!ingress) return false;
        if (ingress.endpointNamespaceKind === "platform") {
          return !endpoint.customDomainId;
        }
        return endpoint.customDomainId === ingress.customDomainId;
      }),
    [endpoints, ingress],
  );
  const featureQuerySuccessful = features.isSuccess;
  const ingressQuerySuccessful = canReadIngress && ingressResult.isSuccess;
  const privateChoicesAvailable =
    featureQuerySuccessful &&
    entitled &&
    ingressQuerySuccessful &&
    ingressOnline &&
    eligibleEndpoints.length > 0;
  const privateStatusPending =
    features.isPending ||
    (featureQuerySuccessful &&
      entitled &&
      canReadIngress &&
      ingressResult.isPending);
  const privateStatusUnavailable =
    (!features.isPending && !featureQuerySuccessful) ||
    !canReadIngress ||
    (featureQuerySuccessful &&
      entitled &&
      canReadIngress &&
      !ingressResult.isPending &&
      !ingressQuerySuccessful);
  const customDomainUrlsResolved =
    !hasCustomDomainEndpoints ||
    (domainsResult.isSuccess &&
      !domainsResult.isFetching &&
      endpoints.every(
        (endpoint) =>
          !endpoint.customDomainId ||
          domains?.some((domain) => domain.id === endpoint.customDomainId) ===
            true,
      ));
  const privateOnlyAvailable =
    privateChoicesAvailable && customDomainUrlsResolved;

  const publicEndpointUrls = useMemo(() => {
    return Array.from(
      new Set(
        endpoints.flatMap((endpoint) => {
          if (!endpoint.customDomainId) {
            return [`${getServerURL()}/mcp/${endpoint.slug}`];
          }

          const domain = domains?.find(
            (candidate) => candidate.id === endpoint.customDomainId,
          );
          if (!domain) {
            return [];
          }

          const endpointUrl = customDomainMcpEndpointUrl(
            domain.domain,
            endpoint.slug,
          );
          return endpoint.isDomainRoot
            ? [`https://${domain.domain}/`, endpointUrl]
            : [endpointUrl];
        }),
      ),
    );
  }, [domains, endpoints]);
  const privateEndpointUrls = useMemo(() => {
    if (!ingress?.dnsName) return [];
    return Array.from(
      new Set(
        eligibleEndpoints.map(
          (endpoint) => `https://${ingress.dnsName}/mcp/${endpoint.slug}`,
        ),
      ),
    );
  }, [eligibleEndpoints, ingress?.dnsName]);

  const update = useUpdateMcpServerMutation({
    onSuccess: async () => {
      setConfirmPrivateOnlyOpen(false);
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Network access updated");
    },
    onError: (error) => {
      setConfirmPrivateOnlyOpen(false);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update network access",
      );
    },
  });

  const save = () => {
    if (
      (draft !== McpServerNetworkAccessMode.PublicOnly &&
        !privateChoicesAvailable) ||
      (draft === McpServerNetworkAccessMode.PrivateOnly &&
        !customDomainUrlsResolved)
    ) {
      return;
    }

    update.mutate({
      request: {
        updateMcpServerForm: {
          id: mcpServer.id,
          networkAccessMode: draft,
          remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
          tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
          toolsetId: mcpServer.toolsetId ?? undefined,
          unproxiedMcpServerId: mcpServer.unproxiedMcpServerId ?? undefined,
          environmentId: mcpServer.environmentId ?? undefined,
          toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
          visibility: mcpServer.visibility,
        },
      },
    });
  };

  const dirty = draft !== mcpServer.networkAccessMode;
  const draftAllowed =
    draft === McpServerNetworkAccessMode.PublicOnly ||
    (draft === McpServerNetworkAccessMode.Dual
      ? privateChoicesAvailable
      : privateOnlyAvailable);
  const footerHint = networkAccessHint({
    entitled,
    ingressOnline,
    hasEligibleEndpoint: eligibleEndpoints.length > 0,
    privateStatusPending,
    privateStatusUnavailable,
    currentMode: mcpServer.networkAccessMode,
  });

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Network access</SettingsSection.Title>
        <SettingsSection.Description>
          Choose whether clients connect through public MCP routes, your private
          network ingress, or both.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field className="max-w-md">
            <FieldLabel htmlFor="mcp-network-access-mode">
              Access mode
            </FieldLabel>
            <Select
              value={draft}
              onValueChange={(value) =>
                setDraft(value as UpdateMcpServerFormNetworkAccessMode)
              }
              disabled={update.isPending}
            >
              <SelectTrigger
                id="mcp-network-access-mode"
                className="w-full"
                aria-label="Network access mode"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  value={McpServerNetworkAccessMode.PublicOnly}
                  description="Serve this MCP server through public routes only"
                >
                  {NETWORK_ACCESS_LABELS.public_only}
                </SelectItem>
                <SelectItem
                  value={McpServerNetworkAccessMode.Dual}
                  disabled={!privateChoicesAvailable}
                  description="Keep public routes and also serve through private ingress"
                >
                  {NETWORK_ACCESS_LABELS.dual}
                </SelectItem>
                <SelectItem
                  value={McpServerNetworkAccessMode.PrivateOnly}
                  disabled={!privateOnlyAvailable}
                  description="Stop public routes and serve only through private ingress"
                >
                  {NETWORK_ACCESS_LABELS.private_only}
                </SelectItem>
              </SelectContent>
            </Select>
            <FieldDescription>
              Private modes are available only while the organization is
              entitled and its private ingress is online.
            </FieldDescription>
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>{footerHint}</SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope
              scope="mcp:write"
              resourceId={mcpServer.projectId}
              level="component"
            >
              <FooterSaveButton
                pending={update.isPending}
                disabled={!dirty || !draftAllowed || update.isPending}
                onClick={() => {
                  if (draft === McpServerNetworkAccessMode.PrivateOnly) {
                    setConfirmPrivateOnlyOpen(true);
                    return;
                  }
                  save();
                }}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>

      <Dialog
        open={confirmPrivateOnlyOpen}
        onOpenChange={setConfirmPrivateOnlyOpen}
      >
        <Dialog.Content className="max-w-lg">
          <Dialog.Header>
            <Dialog.Title>Make this server private only?</Dialog.Title>
            <Dialog.Description>
              This changes how every client reaches this MCP server.
            </Dialog.Description>
          </Dialog.Header>
          <ul className="text-muted-foreground list-disc space-y-2 pl-5 text-sm">
            <li>
              Public routes stop serving this MCP server. There is no public
              fallback.
            </li>
            <li>
              If the tailnet or private ingress loses connectivity, this MCP
              server is unavailable until service returns or you switch back to
              public access.
            </li>
            <li>
              Marketplace and device-agent URLs are not rewritten to the private
              address. Update those clients separately.
            </li>
          </ul>
          {publicEndpointUrls.length > 0 && (
            <div className="space-y-2">
              <Text small>Public endpoint URLs that will stop serving:</Text>
              <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
                {publicEndpointUrls.map((url) => (
                  <li key={url} className="break-all font-mono text-xs">
                    {url}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {privateEndpointUrls.length > 0 && (
            <div className="space-y-2">
              <Text small>Private endpoint URLs that remain available:</Text>
              <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
                {privateEndpointUrls.map((url) => (
                  <li key={url} className="break-all font-mono text-xs">
                    {url}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={update.isPending}
              onClick={() => setConfirmPrivateOnlyOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={update.isPending || !privateOnlyAvailable}
              onClick={save}
            >
              {update.isPending && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Make private only</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}

function networkAccessHint({
  entitled,
  ingressOnline,
  hasEligibleEndpoint,
  privateStatusPending,
  privateStatusUnavailable,
  currentMode,
}: {
  entitled: boolean;
  ingressOnline: boolean;
  hasEligibleEndpoint: boolean;
  privateStatusPending: boolean;
  privateStatusUnavailable: boolean;
  currentMode: McpServer["networkAccessMode"];
}): string {
  if (privateStatusPending) {
    return "Checking private network availability…";
  }
  if (privateStatusUnavailable) {
    return currentMode === McpServerNetworkAccessMode.PublicOnly
      ? "Private network availability could not be checked. An organization admin can verify the ingress."
      : "Private network availability could not be checked. You can still switch to public only.";
  }
  if (!entitled) {
    return currentMode === McpServerNetworkAccessMode.PublicOnly
      ? "Private network access is not enabled for this organization."
      : "Private network access is no longer enabled. You can still switch to public only.";
  }
  if (!ingressOnline) {
    return currentMode === McpServerNetworkAccessMode.PublicOnly
      ? "Bring the organization's private ingress online to enable private access."
      : "Private ingress is offline. You can still switch to public only.";
  }
  if (!hasEligibleEndpoint) {
    return currentMode === McpServerNetworkAccessMode.PublicOnly
      ? "Add an MCP endpoint in the private ingress's pinned namespace before enabling private access."
      : "No live endpoint remains in the pinned namespace. You can still switch to public only.";
  }
  return "Changes apply to new connections.";
}
