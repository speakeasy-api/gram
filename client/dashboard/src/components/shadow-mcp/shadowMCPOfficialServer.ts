import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/**
 * The query flag that opens the Make official sheet on a Shadow MCP
 * server's page, so the list's row action lands the admin in the same flow.
 */
export const SHADOW_MCP_MAKE_OFFICIAL_PARAM = "make-official";

/** A Shadow MCP server's page, with the Make official sheet open. */
export function shadowMCPMakeOfficialHref(serverPageHref: string): string {
  return `${serverPageHref}?${SHADOW_MCP_MAKE_OFFICIAL_PARAM}=1`;
}

/** Whether a row can be made official at all: only a server with a URL can. */
export function canMakeShadowMCPServerOfficial(
  server: Pick<ShadowMCPInventoryServer, "targetKind" | "officialServer">,
): boolean {
  return server.targetKind !== "stdio_command" && !server.officialServer;
}

/**
 * What a linked server's status line says. Direct connections are only
 * blocked where a Shadow MCP policy applies, so with none the line says the
 * direct connection still works rather than claiming a block.
 */
export function shadowMCPOfficialServerDescription(
  server: Pick<ShadowMCPInventoryServer, "officialServer" | "accessSummary">,
): string {
  const official = server.officialServer;
  if (!official) return "";
  const served = official.gatewayName
    ? `Linked to “${official.name}”, served through the gateway “${official.gatewayName}”.`
    : `Linked to “${official.name}”.`;
  if (!official.url) {
    return `${served} It has no address yet, so direct connections still work.`;
  }
  if (official.directAccessAllowed) {
    return `${served} The link is off, so direct connections follow the usual rules.`;
  }
  if (server.accessSummary?.state === "unenforced") {
    return `${served} No Shadow MCP policy is on, so direct connections still work.`;
  }
  return `${served} Direct connections are blocked and agents are sent there.`;
}

/**
 * The state a linked official version puts a Shadow MCP server in.
 * "unlinked" means the link exists but is switched off, which an admin
 * should not forget about, so it reads differently from "onboarded". A
 * server someone is waiting on keeps its pending state.
 */
export function shadowMCPOfficialState(
  server: Pick<ShadowMCPInventoryServer, "officialServer">,
  pending: boolean,
): "onboarded" | "unlinked" | null {
  if (!server.officialServer || pending) return null;
  return server.officialServer.directAccessAllowed ? "unlinked" : "onboarded";
}

export const SHADOW_MCP_OFFICIAL_STATE_LABEL = {
  onboarded: "Onboarded",
  unlinked: "Unlinked",
} as const;

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
    "outcome" | "reason" | "protectedResourceMetadataUrl" | "signInSetup"
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
    detail: "Speakeasy connected to it.",
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
    // The provider decides whether Speakeasy can register itself: by
    // dynamic client registration, or by publishing a client ID metadata
    // document. Without either, someone registers a client by hand.
    if (probe.signInSetup === "manual" || probe.signInSetup === "unknown") {
      const detail =
        probe.signInSetup === "manual"
          ? "Its sign-in provider needs a client registered by hand."
          : "Its sign-in provider's details could not be read.";
      return {
        ready: false,
        blocker: detail,
        blockerCode: `sign_in_${probe.signInSetup}`,
        checks: [
          reachable,
          mcp,
          { id: "sign-in", label: signInLabel, state: "fail", detail },
        ],
      };
    }
    const detail =
      probe.signInSetup === "dcr"
        ? "Its sign-in provider lets Speakeasy register itself (dynamic client registration)."
        : probe.signInSetup === "cimd"
          ? "Its sign-in provider accepts Speakeasy's client ID metadata document."
          : "It names its sign-in provider, so each person signs in once.";
    return {
      ready: true,
      checks: [
        reachable,
        mcp,
        { id: "sign-in", label: signInLabel, state: "pass", detail },
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

/** The catalog fields the Shadow MCP page needs to point at an entry. */
export type ShadowMCPCatalogCandidate = {
  registrySpecifier: string;
  title?: string;
  remotes?: Array<{ url: string }>;
};

/**
 * One URL in the form both lists use: no trailing slash, a lowercase scheme
 * and host, and no default port. Paths stay case sensitive.
 */
function comparableServerUrl(raw: string): string {
  let parsed: URL;
  try {
    parsed = new URL(raw.trim());
  } catch {
    return raw.trim().replace(/\/+$/, "");
  }
  const defaultPort =
    (parsed.protocol === "https:" && parsed.port === "443") ||
    (parsed.protocol === "http:" && parsed.port === "80");
  const host = defaultPort ? parsed.hostname : parsed.host;
  const path = parsed.pathname.replace(/\/+$/, "");
  return `${parsed.protocol}//${host.toLowerCase()}${path}${parsed.search}`;
}

/**
 * The catalog entry that serves this Shadow MCP server's URL, if there is
 * one. A catalog entry carries the setup steps for a server whose sign-in
 * provider needs a client registered by hand, so it is the next step when
 * the server cannot be made official automatically.
 */
export function shadowMCPCatalogMatch<T extends ShadowMCPCatalogCandidate>(
  catalog: readonly T[],
  serverUrl: string,
): T | null {
  const target = comparableServerUrl(serverUrl);
  const match = catalog.find((entry) =>
    (entry.remotes ?? []).some(
      (remote) => comparableServerUrl(remote.url) === target,
    ),
  );
  return match ?? null;
}

/** The remote of a catalog entry that serves this Shadow MCP server's URL. */
export function shadowMCPCatalogRemote<R extends { url: string }>(
  remotes: readonly R[] | undefined,
  serverUrl: string,
): R | null {
  const target = comparableServerUrl(serverUrl);
  return (
    (remotes ?? []).find(
      (remote) => comparableServerUrl(remote.url) === target,
    ) ?? null
  );
}

/**
 * The ways a Shadow MCP server can get an official version, quickest first:
 * link the project server that already serves its URL, add its catalog
 * entry, make a new server when Speakeasy can register itself with the
 * sign-in provider, or set a new one up by hand.
 */
export type OnboardOption = "link" | "catalog" | "quick" | "manual";

/**
 * The one option the page recommends. The others stay available under
 * Advanced. Null while what decides it is still loading, so the page does
 * not recommend one option and then swap it for another.
 */
export function recommendedOnboardOption({
  hasSuggestedServer,
  hasCatalogEntry,
  catalogPending,
  readiness,
}: {
  hasSuggestedServer: boolean;
  hasCatalogEntry: boolean;
  catalogPending: boolean;
  readiness: Pick<OnboardReadiness, "ready"> | null;
}): OnboardOption | null {
  if (hasSuggestedServer) return "link";
  if (catalogPending) return null;
  if (hasCatalogEntry) return "catalog";
  if (!readiness) return null;
  return readiness.ready ? "quick" : "manual";
}
