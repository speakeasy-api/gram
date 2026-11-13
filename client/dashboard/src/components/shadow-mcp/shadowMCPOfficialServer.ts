import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/** The query the "Make official" link carries to the new-server form. */
export const SHADOW_MCP_ONBOARD_SOURCE = "shadow-mcp";

function shadowMCPServerDisplayName(
  server: Pick<
    ShadowMCPInventoryServer,
    "serverName" | "urlHost" | "canonicalServerUrl"
  >,
): string {
  return server.serverName || server.urlHost || server.canonicalServerUrl;
}

/**
 * The new-server form with this server's URL and name filled in. The form
 * checks the connection itself before it saves, so a link taken from the
 * list is as safe as one taken after the checks on the server's page.
 */
export function shadowMCPOnboardHref(
  newRemoteServerHref: string,
  server: Pick<
    ShadowMCPInventoryServer,
    "serverName" | "urlHost" | "canonicalServerUrl"
  >,
): string {
  const params = new URLSearchParams({
    url: server.canonicalServerUrl,
    name: shadowMCPServerDisplayName(server),
    from: SHADOW_MCP_ONBOARD_SOURCE,
  });
  return `${newRemoteServerHref}?${params.toString()}`;
}

/** Whether a row can be made official at all: only a server with a URL can. */
export function canMakeShadowMCPServerOfficial(
  server: Pick<ShadowMCPInventoryServer, "targetKind" | "officialServer">,
): boolean {
  return server.targetKind !== "stdio_command" && !server.officialServer;
}

/**
 * What an onboarded server's status line says. Direct connections are only
 * blocked where a Shadow MCP policy applies, so with none the line says the
 * direct connection still works rather than claiming a block.
 */
export function shadowMCPOfficialServerDescription(
  server: Pick<ShadowMCPInventoryServer, "officialServer" | "accessSummary">,
): string {
  const official = server.officialServer;
  if (!official) return "";
  const served = official.gatewayName
    ? `Served as “${official.name}” through the gateway “${official.gatewayName}”.`
    : `Served as “${official.name}”.`;
  if (!official.url) {
    return `${served} It has no address yet, so direct connections still work.`;
  }
  if (server.accessSummary?.state === "unenforced") {
    return `${served} No Shadow MCP policy is on, so direct connections still work.`;
  }
  return `${served} Direct connections are blocked and agents are sent there.`;
}

type OnboardCheckState = "pass" | "fail" | "skipped";

export type OnboardCheck = {
  id: "reachable" | "mcp" | "sign-in";
  label: string;
  state: OnboardCheckState;
  detail: string;
};

export type OnboardReadiness = {
  /** Every check passed: the new-server form can finish the job. */
  ready: boolean;
  checks: OnboardCheck[];
  /** Why it is not ready, in one sentence, for the note and the request. */
  blocker?: string;
  /** A stable word for the same reason, for whoever handles the request. */
  blockerCode?: string;
};

function unreachableDetail(reason: string | undefined): string {
  switch (reason) {
    case "guardian_rejected":
      return "It is on a private network that Speakeasy cannot reach.";
    case "timeout":
      return "It did not answer in time.";
    case "dns_error":
      return "Its address could not be found.";
    case "tls_error":
      return "Its secure connection failed.";
    case "rate_limited":
      return "It refused the check because of a rate limit.";
    case "server_error":
      return "It answered with a server error.";
    case undefined:
    default:
      return "Speakeasy could not connect to it.";
  }
}

/**
 * Turns one connection check into the three questions an admin has before
 * making a server official: can Speakeasy reach it, is it an MCP server, and
 * can people sign in through Speakeasy. A check that could not run because
 * an earlier one failed is skipped rather than failed.
 */
export function onboardReadiness(
  probe: Pick<
    ProbeURLResult,
    "outcome" | "reason" | "protectedResourceMetadataUrl"
  >,
): OnboardReadiness {
  const reachableLabel = "Speakeasy can reach it";
  const mcpLabel = "It is an MCP server";
  const signInLabel = "Sign-in can be set up";

  if (probe.outcome === "unreachable") {
    const detail = unreachableDetail(probe.reason);
    return {
      ready: false,
      blocker: detail,
      blockerCode: `unreachable:${probe.reason ?? "unknown"}`,
      checks: [
        { id: "reachable", label: reachableLabel, state: "fail", detail },
        { id: "mcp", label: mcpLabel, state: "skipped", detail: "" },
        { id: "sign-in", label: signInLabel, state: "skipped", detail: "" },
      ],
    };
  }

  const reachable: OnboardCheck = {
    id: "reachable",
    label: reachableLabel,
    state: "pass",
    detail: "It is on the public internet.",
  };

  if (probe.outcome === "invalid_mcp_response") {
    const detail = "It answered, but not the way an MCP server does.";
    return {
      ready: false,
      blocker: detail,
      blockerCode: "invalid_mcp_response",
      checks: [
        reachable,
        { id: "mcp", label: mcpLabel, state: "fail", detail },
        { id: "sign-in", label: signInLabel, state: "skipped", detail: "" },
      ],
    };
  }

  const mcp: OnboardCheck = {
    id: "mcp",
    label: mcpLabel,
    state: "pass",
    detail: "",
  };

  if (probe.outcome === "mcp_available") {
    return {
      ready: true,
      checks: [
        reachable,
        mcp,
        {
          id: "sign-in",
          label: signInLabel,
          state: "pass",
          detail: "It needs no sign-in.",
        },
      ],
    };
  }

  if (
    probe.outcome === "authentication_required" &&
    probe.protectedResourceMetadataUrl
  ) {
    return {
      ready: true,
      checks: [
        reachable,
        mcp,
        {
          id: "sign-in",
          label: signInLabel,
          state: "pass",
          detail:
            "It names its sign-in provider, so each person signs in once.",
        },
      ],
    };
  }

  if (probe.outcome === "authentication_required") {
    const detail =
      "It asks for a key or password and does not name a sign-in provider.";
    return {
      ready: false,
      blocker: detail,
      blockerCode: "no_sign_in_provider",
      checks: [
        reachable,
        mcp,
        { id: "sign-in", label: signInLabel, state: "fail", detail },
      ],
    };
  }

  const detail = "The check returned a result this page does not know.";
  return {
    ready: false,
    blocker: detail,
    blockerCode: "unknown_outcome",
    checks: [
      { id: "reachable", label: reachableLabel, state: "skipped", detail: "" },
      { id: "mcp", label: mcpLabel, state: "skipped", detail: "" },
      { id: "sign-in", label: signInLabel, state: "skipped", detail: "" },
    ],
  };
}
