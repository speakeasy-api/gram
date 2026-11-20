import { findPassThroughAuthorizationHeader } from "@/lib/remote-identity/model/headers";
import { useRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders.js";
import {
  useAllRemoteSessionClients,
  useUpstreamProbe,
} from "@/lib/remote-identity";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";

/** Shared by the setup banner, server overview, and official catalog setup. */
export interface McpServerAuthenticationReadiness {
  ready: boolean;
  loading: boolean;
  /** A failed read is unknown, not an open setup step. */
  unavailable: boolean;
  description: string;
  upstreamAuthQuery: ReturnType<typeof useMcpServerAuthentication>;
  needsHeaderCleanup: boolean;
  identityProbeStatus: ReturnType<typeof useUpstreamProbe>;
}

export function useMcpServerAuthenticationReadiness(
  mcpServer: McpServer | undefined,
): McpServerAuthenticationReadiness {
  const remoteMcpServerId = mcpServer?.remoteMcpServerId ?? "";
  const remote = remoteMcpServerId !== "";
  const tunneled = !!mcpServer?.tunneledMcpServerId;
  const unproxied = !!mcpServer?.unproxiedMcpServerId;
  const hosted = !!mcpServer && !remote && !tunneled && !unproxied;
  const userSessionIssuerId = mcpServer?.userSessionIssuerId;
  // This remains a downstream sign-in read for hosted servers only.
  const hostedSignIn = useAllRemoteSessionClients(
    { userSessionIssuerId },
    {
      enabled: hosted && !!userSessionIssuerId,
    },
  );
  const hostedReady =
    hosted &&
    !!userSessionIssuerId &&
    !hostedSignIn.isError &&
    hostedSignIn.items.length > 0;
  const upstreamAuthQuery = useMcpServerAuthentication(
    { mcpServerId: mcpServer?.id ?? "" },
    undefined,
    { enabled: remote || tunneled, throwOnError: false },
  );
  const status = upstreamAuthQuery.data?.status;
  // Legacy source headers are diagnostic only; they do not decide readiness.
  const sourceHeaders = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    {
      enabled:
        remote &&
        (status === "setup_incomplete" || status === "no_auth_required"),
      throwOnError: false,
    },
  );
  const needsHeaderCleanup =
    remote &&
    !sourceHeaders.isError &&
    !upstreamAuthQuery.isError &&
    (status === "setup_incomplete" || status === "no_auth_required") &&
    !!findPassThroughAuthorizationHeader(sourceHeaders.data?.headers ?? []);
  const identityProbeStatus = useUpstreamProbe(
    remoteMcpServerId,
    remote && !upstreamAuthQuery.isError && status === "no_auth_required",
  );
  const knownStatus =
    status === "configured" ||
    status === "setup_incomplete" ||
    status === "no_auth_required";
  const configured = !upstreamAuthQuery.isError && status === "configured";
  const acceptsAnonymous =
    !upstreamAuthQuery.isError &&
    status === "no_auth_required" &&
    identityProbeStatus === "available";

  let description =
    "Attach an authorization server so users can access the upstream service.";
  if (unproxied) {
    description =
      "Not applicable — the customer connects directly using the vendor's own credentials.";
  } else if (remote) {
    description = configured
      ? "Credentials are set up for the upstream service."
      : acceptsAnonymous
        ? "The upstream service accepts requests without credentials."
        : "Add a credential so Speakeasy can sign in to the upstream service.";
  } else if (hostedReady) {
    description = "An authorization server is attached to this server.";
  } else if (tunneled) {
    description =
      "Speakeasy authentication is configured; upstream authorization servers are optional.";
  }

  return {
    ready:
      unproxied ||
      hostedReady ||
      (remote && (configured || acceptsAnonymous)) ||
      (tunneled && !!mcpServer?.userSessionIssuerId),
    loading:
      remote &&
      !upstreamAuthQuery.isError &&
      (upstreamAuthQuery.isPending || identityProbeStatus === "loading"),
    unavailable:
      remote &&
      (upstreamAuthQuery.isError ||
        identityProbeStatus === "unknown" ||
        (status !== undefined && !knownStatus)),
    description,
    upstreamAuthQuery,
    needsHeaderCleanup,
    identityProbeStatus,
  };
}
