import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { AIToolAccessSummary } from "@gram/client/models/components/aitoolaccesssummary.js";

// The access summary is optional on the wire for one release: the detections
// endpoint predates it, so this dashboard can meet a server that does not
// send it yet, during a deploy or after a rollback. A row without one is a
// tool nobody has decided about and that nothing could enforce a decision
// for, which is exactly what the server would say if it could. Read it
// through here so every consumer agrees on that reading.
export function accessOf(detection: AIDetection): AIToolAccessSummary {
  return (
    detection.access ?? {
      state: "unreviewed",
      decision: "unreviewed",
      enforceable: false,
      rationale: undefined,
    }
  );
}

/** A decision is enforced when the tool identifies itself to the MCP gateway,
 *  through a CIMD vendor key or an OAuth client id: the server's rule, kept
 *  here only for the fallback below. */
function isEnforceableTarget(target: AiScanTarget): boolean {
  return (
    target.gatewayClient.cimdVendorKeys.length > 0 ||
    target.gatewayClient.oauthClientIds.length > 0
  );
}

// The scan target's access summary is optional on the wire for one release,
// for the same reason as the detection's: the library endpoint predates it.
// A target without one is read as undecided, and as enforceable by the same
// matcher rule the server applies, so the control is still offered during a
// rollout and the decision lands once the server reports it.
export function scanTargetAccessOf(target: AiScanTarget): AIToolAccessSummary {
  return (
    target.access ?? {
      state: "unreviewed",
      decision: "unreviewed",
      enforceable: isEnforceableTarget(target),
      rationale: undefined,
    }
  );
}
