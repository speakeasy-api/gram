import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { ruleCategoryMeta } from "@/pages/security/policy-data";
import { ServerGuardrailsForm } from "@/pages/security/server-guardrails/ServerGuardrailsForm";
import { effectiveAction } from "@/pages/security/server-guardrails/server-guardrail-policy";
import { useDetectorMode } from "@/pages/security/use-detector-mode";
import { useState } from "react";
import type { GuardrailsPhase } from "./useRemoteMcpInstallWorkflow";

const ACTION_LABELS = {
  flag: "Log for review",
  warn: "Warn & confirm",
  block: "Deny the request",
  quarantine: "Quarantine session",
} as const;

/** The catalog install's skippable Guardrails step: a pre-filled, scoped
 *  guardrail shown as a summary, with the full form one click away. */
export function CatalogGuardrailsPhase({
  releaseState,
  onClose,
}: {
  releaseState: GuardrailsPhase;
  onClose: () => void;
}): JSX.Element {
  const mode = useDetectorMode();
  const [customizing, setCustomizing] = useState(false);
  const { guardrail, serverNames } = releaseState;
  const scopeName =
    serverNames.length === 1
      ? serverNames[0]!
      : `${serverNames.length} servers`;
  const detectors = [...guardrail.categories]
    .map((category) => ruleCategoryMeta(category, mode).label)
    .join(", ");

  return (
    <div className="space-y-4">
      <div className="border-border space-y-3 border p-4">
        <div>
          <div className="text-sm font-medium">
            Create a risk policy for {scopeName}
          </div>
          <Text small muted>
            Scoped to{" "}
            {serverNames.length === 1 ? "this server" : "these servers"} and
            their tools only. Suggested from the catalog&apos;s tool
            annotations.
          </Text>
        </div>
        <dl className="grid grid-cols-[6rem_1fr] gap-x-4 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">Detect</dt>
          <dd>{detectors || "Nothing selected"}</dd>
          <dt className="text-muted-foreground">Tools</dt>
          <dd>All tools</dd>
          <dt className="text-muted-foreground">Inspects</dt>
          <dd>Tool requests · Tool responses</dd>
          <dt className="text-muted-foreground">Action</dt>
          <dd>
            {ACTION_LABELS[effectiveAction(guardrail)]} · Severity{" "}
            {guardrail.score.toFixed(1)}
          </dd>
          <dt className="text-muted-foreground">Audience</dt>
          <dd>
            {guardrail.audienceType === "everyone"
              ? "Everyone"
              : "Targeted users or roles"}
          </dd>
        </dl>
        <Text small muted>
          Adjust detectors, tools or action later on the server&apos;s
          Guardrails tab, or{" "}
          <button
            type="button"
            className="underline underline-offset-2"
            onClick={() => setCustomizing((open) => !open)}
          >
            {customizing ? "hide options" : "customize now"}
          </button>
          .
        </Text>
      </div>
      {customizing ? (
        <div className="max-h-[50vh] overflow-y-auto pr-1">
          <ServerGuardrailsForm
            state={guardrail}
            onChange={releaseState.updateGuardrail}
            toolsSource={{ status: "unavailable" }}
            serverName={scopeName}
            mode={mode}
          />
        </div>
      ) : null}
      <Dialog.Footer>
        <div className="flex gap-2">
          {releaseState.goBack ? (
            <Button variant="tertiary" onClick={releaseState.goBack}>
              Back
            </Button>
          ) : null}
          <Button variant="tertiary" onClick={onClose}>
            Cancel
          </Button>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => void releaseState.skip()}>
            <Button.Text>Skip for now</Button.Text>
          </Button>
          <Button
            disabled={guardrail.categories.size === 0}
            onClick={() => void releaseState.installWithGuardrail()}
          >
            <Button.Text>Add to Project</Button.Text>
          </Button>
        </div>
      </Dialog.Footer>
    </div>
  );
}
