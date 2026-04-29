import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import {
  useNlPoliciesGetReplayRun,
  useNlPoliciesListReplayResults,
  useNlPoliciesReplayMutation,
} from "@gram/client/react-query/index.js";
import type { NLPolicy } from "@gram/client/models/components/nlpolicy.js";
import { Loader2 } from "lucide-react";
import { useState } from "react";

type Counts = {
  would_block?: number;
  would_allow?: number;
  judge_error?: number;
};

function safeParseCounts(raw: string | undefined): Counts | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (typeof parsed === "object" && parsed !== null) return parsed as Counts;
    return null;
  } catch {
    return null;
  }
}

export default function NLPolicyReplayModal({
  policy,
  onClose,
}: {
  policy: NLPolicy;
  onClose: () => void;
}) {
  const [windowDays, setWindowDays] = useState(7);
  const [sampleSize, setSampleSize] = useState(100);
  const [scope, setScope] = useState<"per_call" | "session">("per_call");
  const [runId, setRunId] = useState<string | null>(null);

  const replay = useNlPoliciesReplayMutation({
    onSuccess: (run) => setRunId(run.id),
  });

  const { data: run } = useNlPoliciesGetReplayRun(
    { runId: runId ?? "" },
    undefined,
    {
      enabled: !!runId,
      refetchInterval: (query) => {
        const status = query.state.data?.status;
        return status === "completed" || status === "failed" ? false : 2000;
      },
    },
  );

  const { data: results } = useNlPoliciesListReplayResults(
    { runId: runId ?? "" },
    undefined,
    { enabled: !!runId && run?.status === "completed" },
  );

  const counts = safeParseCounts(run?.counts);
  const isStarting = replay.isPending;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <Dialog.Content className="sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Run replay</Dialog.Title>
          <Dialog.Description>
            Replay the policy against historical chat traffic without
            enforcement.
          </Dialog.Description>
        </Dialog.Header>

        {!runId && (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>Sample window</Label>
              <select
                className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                value={windowDays}
                onChange={(e) =>
                  setWindowDays(parseInt(e.target.value, 10) || 7)
                }
              >
                <option value={1}>Last 24 hours</option>
                <option value={7}>Last 7 days</option>
                <option value={30}>Last 30 days</option>
              </select>
            </div>
            <div className="space-y-2">
              <Label>Sample size</Label>
              <input
                type="number"
                className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                value={sampleSize}
                min={1}
                max={1000}
                onChange={(e) =>
                  setSampleSize(parseInt(e.target.value, 10) || 100)
                }
              />
              <Type small muted>
                Maximum 1000 calls per replay.
              </Type>
            </div>
            <div className="space-y-2">
              <Label>Scope</Label>
              <select
                className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                value={scope}
                onChange={(e) =>
                  setScope(e.target.value as "per_call" | "session")
                }
              >
                <option value="per_call">Per call</option>
                <option value="session">Session</option>
              </select>
            </div>
            <Dialog.Footer>
              <Button variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                onClick={() =>
                  replay.mutate({
                    request: {
                      replayRequestBody: {
                        policyId: policy.id,
                        sampleFilter: JSON.stringify({
                          window_days: windowDays,
                          sample_size: sampleSize,
                          scope,
                        }),
                      },
                    },
                  })
                }
                disabled={isStarting}
              >
                {isStarting && (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                )}
                Run replay
              </Button>
            </Dialog.Footer>
          </div>
        )}

        {runId && (
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <Type small muted className="font-mono">
                Replay {runId}
              </Type>
              <Badge variant="outline">{run?.status ?? "starting"}</Badge>
            </div>

            {counts && (
              <div className="flex flex-wrap gap-2">
                <Badge variant="destructive">
                  Would BLOCK: {counts.would_block ?? 0}
                </Badge>
                <Badge>Would ALLOW: {counts.would_allow ?? 0}</Badge>
                <Badge variant="secondary">
                  JUDGE_ERROR: {counts.judge_error ?? 0}
                </Badge>
              </div>
            )}

            {run?.status === "completed" && results && (
              <div className="max-h-80 overflow-auto rounded border">
                <table className="w-full text-sm">
                  <thead className="bg-muted/50">
                    <tr>
                      <th className="p-2 text-left">Decision</th>
                      <th className="p-2 text-left">Tool</th>
                      <th className="p-2 text-left">Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(results.results ?? []).map((r) => (
                      <tr key={r.id} className="border-t">
                        <td className="p-2">
                          <Badge
                            variant={
                              r.decision === "BLOCK"
                                ? "destructive"
                                : r.decision === "JUDGE_ERROR"
                                  ? "secondary"
                                  : "default"
                            }
                          >
                            {r.decision}
                          </Badge>
                        </td>
                        <td className="p-2 font-mono text-xs">
                          {r.toolUrn ?? "—"}
                        </td>
                        <td className="p-2 text-xs">{r.reason ?? "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {run?.status === "failed" && (
              <Type small muted>
                Replay failed.
              </Type>
            )}

            <Dialog.Footer>
              <Button onClick={onClose}>Close</Button>
            </Dialog.Footer>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
