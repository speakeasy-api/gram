import type {
  EnforcementOutcome,
  RiskResult,
} from "@gram/client/models/components/riskresult.js";
import { isBlockingOutcome } from "./risk-outcome";

export type CallPathLink = { blocked: boolean; label: string };

export type CallPath = {
  /** Client ↔ gateway. */
  left: CallPathLink;
  /** Gateway ↔ MCP server. */
  right: CallPathLink;
  /** False when the gateway stopped the request before it reached the server. */
  serverReached: boolean;
  caption: string;
};

const HOLD_LABEL: Partial<Record<EnforcementOutcome, string>> = {
  denied: "denied",
  withheld: "withheld",
  quarantined: "quarantined",
  warned_pending: "held",
  warned_abandoned: "abandoned",
};

function requestCaption(outcome: EnforcementOutcome, server: string): string {
  switch (outcome) {
    case "denied":
    case "withheld":
      return `The gateway stopped this tools/call before it reached ${server}. The client received a policy error.`;
    case "quarantined":
      return `The gateway quarantined this tools/call before it reached ${server}.`;
    case "warned_pending":
      return `The gateway is holding this tools/call until the user confirms the warning.`;
    case "warned_abandoned":
      return `The user was warned and abandoned the call. It never reached ${server}.`;
    case "warned_acknowledged":
      return `The user was warned and confirmed. The call was forwarded to ${server}.`;
    case "logged":
      return `Logged only. The call was forwarded to ${server} unchanged.`;
  }
}

function responseCaption(outcome: EnforcementOutcome, server: string): string {
  switch (outcome) {
    case "denied":
    case "withheld":
      return `${server} returned a result; the gateway withheld it from the client.`;
    case "quarantined":
      return `${server} returned a result; the gateway quarantined it.`;
    case "warned_pending":
      return `${server} returned a result; the gateway is holding it until the user confirms the warning.`;
    case "warned_abandoned":
      return `${server} returned a result; the user was warned and abandoned it.`;
    case "warned_acknowledged":
      return `${server} returned a result; the user was warned and confirmed delivery.`;
    case "logged":
      return `Logged only. The result from ${server} reached the client unchanged.`;
  }
}

/**
 * The outcome of the call a finding was raised in. A logged finding still sits
 * on a denied call when another policy blocked the same execution phase.
 */
export function callOutcome(
  result: RiskResult,
  siblings: readonly RiskResult[],
): EnforcementOutcome | undefined {
  const blocking = siblings.find(
    (s) =>
      s.executionId === result.executionId &&
      s.phase === result.phase &&
      isBlockingOutcome(s.enforcementOutcome),
  );
  return blocking?.enforcementOutcome ?? result.enforcementOutcome;
}

// Maps phase × enforcement outcome to the call path strip: the blocked link is
// wherever the gateway held the traffic.
export function callPathFor(
  phase: string | undefined,
  outcome: EnforcementOutcome | undefined,
  server: string,
): CallPath {
  const holdLabel = outcome ? HOLD_LABEL[outcome] : undefined;
  if (phase === "response") {
    return {
      left: holdLabel
        ? { blocked: true, label: holdLabel }
        : { blocked: false, label: "← delivered" },
      right: { blocked: false, label: "← response" },
      serverReached: true,
      caption: outcome
        ? responseCaption(outcome, server)
        : `The gateway scanned the result ${server} returned.`,
    };
  }
  return {
    left: { blocked: false, label: "request →" },
    right: holdLabel
      ? { blocked: true, label: holdLabel }
      : { blocked: false, label: "forwarded →" },
    serverReached: !holdLabel,
    caption: outcome
      ? requestCaption(outcome, server)
      : `The gateway scanned this tools/call on its way to ${server}.`,
  };
}
