// New eval run sheet (AGE-2704) — sampler config + confirm.
//
// Extracted from EvalsTab so both the Evals tab and the Configuration tab's
// eval-signal banner open the same sheet. The request is built as exactly one of
// {policyId}/{candidate} (from evalSource) plus the sample definition.

import { Type } from "@/components/ui/type";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Input } from "@/components/ui/input";
import { Slider } from "@/components/ui/slider";
import { Label } from "@/components/ui/label";
import { Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskListPolicyEvalRuns,
  useRiskCreatePolicyEvalRunMutation,
} from "@gram/client/react-query/index.js";
import type { PolicyEvalRun } from "@gram/client/models/components/policyevalrun.js";
import type { EvalSource } from "../policy-form/use-policy-form";

// Bounds on the number of historical messages a single eval run will replay.
const MAX_SAMPLE_SIZE = 50000;
const MIN_SAMPLE_SIZE = 1;

export function NewRunSheet({
  open,
  onOpenChange,
  evalSource,
  onCreated,
  canRun = true,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  evalSource: EvalSource;
  /** Called with the created run before the sheet closes, so the caller can
   *  immediately surface the candidate run (which is otherwise invisible). */
  onCreated?: (run: PolicyEvalRun) => void;
  /** When false, the config has nothing to evaluate; Start is disabled. */
  canRun?: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [sampleSize, setSampleSize] = useState(2000);
  const [lookbackDays, setLookbackDays] = useState(30);

  const createMutation = useRiskCreatePolicyEvalRunMutation({
    onSuccess: (data) => {
      onCreated?.(data);
      void invalidateAllRiskListPolicyEvalRuns(queryClient);
      onOpenChange(false);
    },
  });
  const runError = createMutation.error;

  const sampleSizeInvalid =
    !Number.isFinite(sampleSize) ||
    sampleSize < MIN_SAMPLE_SIZE ||
    sampleSize > MAX_SAMPLE_SIZE;
  const startDisabled =
    createMutation.isPending || !canRun || sampleSizeInvalid;

  const handleConfirm = () => {
    // Exactly one of policyId / candidate, plus the sample definition.
    const source =
      "policyId" in evalSource
        ? { policyId: evalSource.policyId }
        : { candidate: evalSource.candidate };
    createMutation.mutate({
      request: {
        createPolicyEvalRunRequestBody: {
          ...source,
          sample: {
            mode: "auto",
            maxMessages: sampleSize,
            lookbackDays,
          },
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col">
        <SheetHeader>
          <SheetTitle>New eval run</SheetTitle>
          <SheetDescription>
            Configure how this policy is replayed over historical messages.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-4">
          <div className="space-y-2">
            <Label>Max messages to replay</Label>
            <Input
              type="number"
              min={MIN_SAMPLE_SIZE}
              max={MAX_SAMPLE_SIZE}
              value={String(sampleSize)}
              onChange={(v) => setSampleSize(Number(v) || 0)}
            />
            {sampleSizeInvalid ? (
              <Type small className="text-destructive font-normal">
                Enter a number between {MIN_SAMPLE_SIZE.toLocaleString()} and{" "}
                {MAX_SAMPLE_SIZE.toLocaleString()}.
              </Type>
            ) : (
              <Type small muted className="font-normal">
                Maximum number of historical messages to replay.
              </Type>
            )}
          </div>

          <div className="space-y-2">
            <Label>Lookback window: {lookbackDays} days</Label>
            <Slider
              min={1}
              max={90}
              step={1}
              value={lookbackDays}
              onChange={(v) => setLookbackDays(Math.round(v))}
            />
          </div>
        </div>

        {!canRun && (
          <Type small muted className="mx-4 font-normal">
            Add at least one detector / a prompt before running an eval.
          </Type>
        )}

        {runError && (
          <div
            role="alert"
            className="border-destructive/40 bg-destructive/10 text-destructive mx-4 rounded-md border px-3 py-2 text-sm"
          >
            Couldn't start the run:{" "}
            {runError instanceof Error ? runError.message : "unexpected error"}.
            Please try again.
          </div>
        )}

        <SheetFooter>
          <Button
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={createMutation.isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button onClick={handleConfirm} disabled={startDisabled}>
            <Button.Text>
              {createMutation.isPending ? "Starting…" : "Start run"}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
