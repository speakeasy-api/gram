import { useRBAC } from "@/hooks/useRBAC";
import {
  gitHubAppBinding,
  type GitHubAppBinding,
} from "@/lib/remote-identity/model/githubApp";
import { useServerClientAssociations } from "@gram/client/react-query/serverClientAssociations.js";
import {
  deriveIdentityMode,
  findPassThroughAuthorizationHeader,
  type IdentityMode,
  useAllRemoteSessionClients,
  useUpstreamProbe,
} from "@/lib/remote-identity";
import { useUpstreamAuthEnforcement } from "@/hooks/useUpstreamAuthEnforcement";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders.js";

/**
 * Whether an MCP server's Authentication setup step is done, and the line that
 * describes it. The server sidebar's readiness bar and the settings page's
 * setup banner both read this, so they never disagree about what is left.
 */
export interface McpServerAuthenticationReadiness {
  /** Whether the Authentication step is done. */
  ready: boolean;
  /** Whether `ready` can still change once the identity queries settle. */
  loading: boolean;
  /**
   * Whether readiness cannot be known because an identity query failed. When
   * true, `ready: false` means "unknown", not "the step is open".
   */
  unavailable: boolean;
  /** One line on the step's state, as the readiness bar shows it. */
  description: string;
  remoteIdentityMode: IdentityMode;
  passThroughAuthorization: ReturnType<
    typeof findPassThroughAuthorizationHeader
  >;
  identityUnavailable: boolean;
  isLoadingRemoteIdentity: boolean;
  upstreamAuth: ReturnType<typeof useUpstreamAuthEnforcement>;
  upstreamAuthQuery: ReturnType<typeof useMcpServerAuthentication>;
  identityProbeStatus: ReturnType<typeof useUpstreamProbe>;
}

export function useMcpServerAuthenticationReadiness(
  mcpServer: McpServer | undefined,
): McpServerAuthenticationReadiness {
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const upstreamAuth = useUpstreamAuthEnforcement();
  const remoteMcpServerId = mcpServer?.remoteMcpServerId ?? "";
  const userSessionIssuerId = mcpServer?.userSessionIssuerId;
  // A remote identity provider is attached when this server's issuer has at
  // least one remote session client pairing.
  const {
    items: remoteSessionClients,
    isLoading: isLoadingRemoteSessionClients,
    isError: isRemoteSessionClientsError,
  } = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { enabled: !!userSessionIssuerId },
  );
  const hasRemoteIdentityProvider = remoteSessionClients.length > 0;
  const {
    data: remoteHeadersResult,
    isLoading: isLoadingRemoteHeaders,
    isError: isRemoteHeadersError,
  } = useRemoteMcpServerHeaders({ remoteMcpServerId }, undefined, {
    enabled: remoteMcpServerId !== "",
  });
  const remoteHeaders = remoteHeadersResult?.headers ?? [];
  const canReadBinding = !rbacLoading && hasScope("mcp:read", mcpServer?.id);
  const associationsQuery = useServerClientAssociations(
    { mcpServerId: mcpServer?.id ?? "" },
    undefined,
    {
      enabled:
        remoteMcpServerId !== "" && upstreamAuth === "legacy" && canReadBinding,
      throwOnError: false,
    },
  );
  const githubBinding = canReadBinding
    ? gitHubAppBinding(associationsQuery.data)
    : null;
  const remoteIdentityMode = deriveIdentityMode(
    remoteSessionClients.length,
    remoteHeaders,
    !!githubBinding?.githubAppBound && githubBinding.githubAppEnabled,
  );
  const passThroughAuthorization =
    findPassThroughAuthorizationHeader(remoteHeaders);
  const identityUnavailable =
    isRemoteSessionClientsError ||
    isRemoteHeadersError ||
    (remoteMcpServerId !== "" &&
      upstreamAuth === "legacy" &&
      (associationsQuery.isError ||
        gitHubAppIdentityUnavailable(githubBinding)));
  // Enforcing organizations configure upstream identity through credentials,
  // so the summary reads their status instead of the legacy identity mode.
  const enforcedIdentity =
    remoteMcpServerId !== "" && upstreamAuth === "enforced";
  const upstreamAuthQuery = useMcpServerAuthentication(
    { mcpServerId: mcpServer?.id ?? "" },
    undefined,
    { enabled: enforcedIdentity && !!mcpServer?.id, throwOnError: false },
  );
  // Only worth probing when nothing is configured: that is the one state where
  // the upstream's answer changes what the pill should say.
  const identityProbeStatus = useUpstreamProbe(
    remoteMcpServerId,
    enforcedIdentity
      ? upstreamAuthQuery.data?.status === "no_auth_required"
      : upstreamAuth === "legacy" &&
          !rbacLoading &&
          !identityUnavailable &&
          remoteIdentityMode === "none" &&
          !passThroughAuthorization &&
          !isLoadingRemoteSessionClients &&
          !isLoadingRemoteHeaders &&
          !associationsQuery.isLoading,
  );

  const isRemoteBacked = !!mcpServer?.remoteMcpServerId;
  const isTunneledBacked = !!mcpServer?.tunneledMcpServerId;
  const isUnproxied = !!mcpServer?.unproxiedMcpServerId;

  // A Remote MCP server's identity is derived, so the readiness item reads the
  // same answer the pill does. Judging it on a bound client alone reported
  // every working Service Account as incomplete while the pill said Service Account.
  // Under enforcement it reads the credentials status instead, and a remote
  // server is not settled until the enforcement state is known.
  const legacyRemoteIdentity = !isRemoteBacked || upstreamAuth === "legacy";
  const enforcedStatus = upstreamAuthQuery.data?.status;
  const remoteIdentitySettled =
    isRemoteBacked &&
    (enforcedIdentity
      ? enforcedStatus === "configured" ||
        (enforcedStatus === "no_auth_required" &&
          identityProbeStatus === "available")
      : legacyRemoteIdentity &&
        !identityUnavailable &&
        (remoteIdentityMode !== "none" || identityProbeStatus === "available"));

  let description =
    "Attach an authorization server so users can access the upstream service.";
  if (isUnproxied) {
    description =
      "Not applicable — the customer connects directly using the vendor's own credentials.";
  } else if (enforcedIdentity) {
    if (enforcedStatus === "configured") {
      description = "Credentials are set up for the upstream service.";
    } else if (remoteIdentitySettled) {
      description =
        "The upstream service accepts requests without credentials.";
    } else {
      description =
        "Add a credential so Speakeasy can sign in to the upstream service.";
    }
  } else if (!legacyRemoteIdentity) {
    description = "The identity status is not available yet.";
  } else if (
    githubBinding?.githubAppRequired &&
    !githubBinding.githubAppBound
  ) {
    description =
      "Callers' requests are blocked until you save a new identity.";
  } else if (remoteIdentityMode === "user" && hasRemoteIdentityProvider) {
    description = "An authorization server is attached to this server.";
  } else if (remoteIdentityMode === "agent" && isRemoteBacked) {
    description = "A shared credential is configured for the upstream service.";
  } else if (remoteIdentitySettled) {
    description = "The upstream service accepts requests without credentials.";
  } else if (isTunneledBacked) {
    description =
      "Speakeasy authentication is configured; upstream authorization servers are optional.";
  }

  const ready =
    isUnproxied ||
    remoteIdentitySettled ||
    (!isRemoteBacked && legacyRemoteIdentity && hasRemoteIdentityProvider) ||
    (isTunneledBacked && !!userSessionIssuerId);

  const isLoadingRemoteIdentity =
    isLoadingRemoteSessionClients ||
    isLoadingRemoteHeaders ||
    rbacLoading ||
    associationsQuery.isLoading;
  // Whether the answer can still change once the queries settle, so a caller
  // never reports a step as open, or done, before the data is in.
  const loading =
    isRemoteBacked &&
    (upstreamAuth === "loading" ||
      (enforcedIdentity
        ? upstreamAuthQuery.isPending
        : isLoadingRemoteIdentity) ||
      identityProbeStatus === "loading");
  const unavailable =
    isRemoteBacked &&
    (upstreamAuth === "error" ||
      (enforcedIdentity ? upstreamAuthQuery.isError : identityUnavailable));

  return {
    ready,
    loading,
    unavailable,
    description,
    remoteIdentityMode,
    passThroughAuthorization,
    identityUnavailable,
    isLoadingRemoteIdentity,
    upstreamAuth,
    upstreamAuthQuery,
    identityProbeStatus,
  };
}

/** Unknown metadata or an unmet requirement must never be probed as anonymous. */
export function gitHubAppIdentityUnavailable(
  binding: GitHubAppBinding | null,
): boolean {
  return (
    !binding ||
    (binding.githubAppBound && !binding.githubAppEnabled) ||
    (binding.githubAppRequired &&
      !(binding.githubAppBound && binding.githubAppEnabled))
  );
}
