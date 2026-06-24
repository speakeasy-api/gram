// Eval-signal banner (AGE-2704).
//
// Compact banner at the top of the Configuration tab that surfaces the latest
// eval result (or a recommendation to run one) without leaving for the Evals
// tab. Evals are advisory: this never blocks enabling a policy. It opens the
// same NewRunSheet the Evals tab uses, driven by `evalSource`.

import { Type } from "@/components/ui/type";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { FlaskConical } from "lucide-react";
import { useState } from "react";
import { useRiskListPolicyEvalRuns } from "@gram/client/react-query/index.js";
import type { PolicyEvalRun } from "@gram/client/models/components/policyevalrun.js";
import type { EvalSource } from "../policy-form/use-policy-form";
import { NewRunSheet } from "./NewRunSheet";
import { formatCount, isRunStale } from "./format";

export function EvalSignalBanner({
  evalSource,
  policyId,
  currentVersion,
  onViewResults,
  onCreated,
  canRun,
}: {
  /** What to evaluate: a saved policy_id (clean) or an inline candidate. */
  evalSource: EvalSource;
  /** The saved policy id, if any. Absent in create/draft mode. */
  policyId?: string;
  /** Current policy version, used to flag a stale latest run. */
  currentVersion?: number;
  /** Jump to the Evals tab to inspect a run in full (the latest, when given). */
  onViewResults: (runId?: string) => void;
  /** Surface the just-created run (forwarded to the run sheet). */
  onCreated?: (run: PolicyEvalRun) => void;
  /** False when the config has nothing to evaluate; gates Start in the sheet. */
  canRun?: boolean;
}): JSX.Element {
  const [newRunOpen, setNewRunOpen] = useState(false);

  // Only a saved policy has a run history; runs come back newest-first.
  const { data } = useRiskListPolicyEvalRuns(
    policyId ? { policyId } : undefined,
    undefined,
    { enabled: policyId != null },
  );
  const latestCompleted = (data?.runs ?? []).find(
    (run) => run.status === "completed",
  );
  const stale =
    latestCompleted != null && isRunStale(latestCompleted, currentVersion);

  return (
    <div className="border-border bg-muted/20 mb-6 flex items-center justify-between gap-4 rounded-lg border p-4">
      <div className="flex min-w-0 items-center gap-3">
        <FlaskConical className="text-muted-foreground h-5 w-5 shrink-0" />
        <div className="min-w-0">
          {latestCompleted ? (
            <>
              <div className="flex items-center gap-2 text-sm font-medium">
                Last eval: {formatCount(latestCompleted.findingsCount)} findings
                across {formatCount(latestCompleted.messagesScanned)} messages
                {stale && (
                  <Badge variant="warning">
                    <Badge.Text>Older config</Badge.Text>
                  </Badge>
                )}
              </div>
              <Type small muted className="font-normal">
                {stale
                  ? "Your last eval ran on an older configuration. Re-run to preview the current policy."
                  : "Replay this policy across recent sessions to preview what it would flag."}
              </Type>
            </>
          ) : (
            <>
              <div className="text-sm font-medium">No evals run yet</div>
              <Type small muted className="font-normal">
                Optional: preview what this policy would flag.
              </Type>
            </>
          )}
        </div>
      </div>

      <div className="flex shrink-0 gap-2">
        {latestCompleted && (
          <Button
            variant="secondary"
            onClick={() => onViewResults(latestCompleted.id)}
          >
            <Button.Text>View results</Button.Text>
          </Button>
        )}
        <Button variant="secondary" onClick={() => setNewRunOpen(true)}>
          <Button.Text>{latestCompleted ? "Re-run" : "Run eval"}</Button.Text>
        </Button>
      </div>

      <NewRunSheet
        open={newRunOpen}
        onOpenChange={setNewRunOpen}
        evalSource={evalSource}
        onCreated={onCreated}
        canRun={canRun}
      />
    </div>
  );
}
