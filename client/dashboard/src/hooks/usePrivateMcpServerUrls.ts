import { useNetworkIngressRollout } from "@/hooks/useNetworkIngressRollout";
import { getServerURL } from "@/lib/utils";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { NetworkIngress } from "@gram/client/models/components/networkingress.js";
import {
  McpServerNetworkAccessMode,
  type McpServer,
} from "@gram/client/models/components/mcpserver.js";
import { useNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import { useMemo } from "react";

export function endpointUsesPrivateIngress(
  endpoint: McpEndpoint,
  ingress: Pick<NetworkIngress, "endpointNamespaceKind" | "customDomainId">,
): boolean {
  if (ingress.endpointNamespaceKind === "platform") {
    return !endpoint.customDomainId;
  }

  return endpoint.customDomainId === ingress.customDomainId;
}

function privateIngressEndpoints(
  ingress: Pick<NetworkIngress, "endpointNamespaceKind" | "customDomainId">,
  endpoints: McpEndpoint[],
): McpEndpoint[] {
  return endpoints.filter((endpoint) =>
    endpointUsesPrivateIngress(endpoint, ingress),
  );
}

export function privateMcpEndpointUrls(
  ingress:
    | Pick<
        NetworkIngress,
        | "dnsName"
        | "endpointNamespaceKind"
        | "customDomainId"
        | "enabled"
        | "status"
      >
    | undefined,
  endpoints: McpEndpoint[],
): string[] {
  if (!ingress?.dnsName || !ingress.enabled || ingress.status !== "online") {
    return [];
  }

  return Array.from(
    new Set(
      privateIngressEndpoints(ingress, endpoints).map(
        (endpoint) =>
          `https://${ingress.dnsName}/mcp/${encodeURIComponent(endpoint.slug)}`,
      ),
    ),
  );
}

export function privateMcpInstallPageUrls(
  ingress:
    | Pick<
        NetworkIngress,
        | "dnsName"
        | "endpointNamespaceKind"
        | "customDomainId"
        | "enabled"
        | "status"
      >
    | undefined,
  endpoints: McpEndpoint[],
): string[] {
  if (!ingress?.dnsName || !ingress.enabled || ingress.status !== "online") {
    return [];
  }

  return Array.from(
    new Set(
      privateIngressEndpoints(ingress, endpoints).map(
        (endpoint) =>
          `${getServerURL()}/mcp/${encodeURIComponent(endpoint.slug)}/install?network=private`,
      ),
    ),
  );
}

export function mcpServerInstallPageLinks(
  networkAccessMode: McpServer["networkAccessMode"] | undefined,
  publicInstallPageUrl: string | undefined,
  privateInstallPageUrls: string[],
): Array<{ url: string; label: string }> {
  const publicRoutesEnabled =
    networkAccessMode !== McpServerNetworkAccessMode.PrivateOnly;
  const privateRoutesEnabled =
    networkAccessMode === McpServerNetworkAccessMode.Dual ||
    networkAccessMode === McpServerNetworkAccessMode.PrivateOnly;
  const links: Array<{ url: string; label: string }> = [];

  if (publicRoutesEnabled && publicInstallPageUrl) {
    links.push({ url: publicInstallPageUrl, label: "Public install page" });
  }
  if (privateRoutesEnabled) {
    links.push(
      ...privateInstallPageUrls.map((url, index) => ({
        url,
        label:
          index === 0
            ? "Private install page"
            : `Private install page ${index + 1}`,
      })),
    );
  }

  return links;
}

export function usePrivateMcpServerUrls(
  mcpServer: Pick<McpServer, "networkAccessMode"> | undefined,
  endpoints: McpEndpoint[],
): {
  privateMcpUrls: string[];
  privateInstallPageUrls: string[];
  canReadPrivateUrls: boolean;
  isLoading: boolean;
  isError: boolean;
} {
  const { rolloutEnabled, canManageIngress } = useNetworkIngressRollout();
  const privateMode =
    mcpServer?.networkAccessMode === McpServerNetworkAccessMode.Dual ||
    mcpServer?.networkAccessMode === McpServerNetworkAccessMode.PrivateOnly;
  const queryEnabled = rolloutEnabled && canManageIngress && privateMode;
  const ingressResult = useNetworkIngress(undefined, undefined, {
    enabled: queryEnabled,
    retry: false,
    throwOnError: false,
  });
  const privateMcpUrls = useMemo(
    () =>
      queryEnabled && ingressResult.isSuccess
        ? privateMcpEndpointUrls(ingressResult.data?.ingress, endpoints)
        : [],
    [
      endpoints,
      ingressResult.data?.ingress,
      ingressResult.isSuccess,
      queryEnabled,
    ],
  );

  return {
    privateMcpUrls,
    privateInstallPageUrls:
      queryEnabled && ingressResult.isSuccess
        ? privateMcpInstallPageUrls(ingressResult.data?.ingress, endpoints)
        : [],
    canReadPrivateUrls: rolloutEnabled && canManageIngress,
    isLoading: queryEnabled && ingressResult.isPending,
    isError: queryEnabled && ingressResult.isError,
  };
}
