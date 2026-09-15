import type { AIDetection } from "@gram/client/models/components/aidetection.js";
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
