import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import {
  guardrailFailureMessage,
  type NewServerGuardrailOutcome,
} from "./useNewServerGuardrail";

// The result of the guardrail created alongside a new server, shown on the
// create flow's confirmation screen. Nothing renders when none was requested.
export function NewServerGuardrailOutcomeAlert({
  outcome,
  onOpenGuardrails,
}: {
  outcome: NewServerGuardrailOutcome | null;
  onOpenGuardrails: () => void;
}): JSX.Element | null {
  if (outcome?.status === "failed") {
    return (
      <Stack gap={2}>
        <Alert variant="error" dismissible={false} alignTop>
          {guardrailFailureMessage(outcome)}
        </Alert>
        <div>
          <Button variant="secondary" onClick={onOpenGuardrails}>
            <Button.Text>Open Guardrails</Button.Text>
          </Button>
        </div>
      </Stack>
    );
  }
  if (outcome?.status === "created") {
    return (
      <Alert variant="success" dismissible={false}>
        Guardrail &quot;{outcome.name}&quot; created. Review it under the
        server&apos;s Guardrails tab.
      </Alert>
    );
  }
  return null;
}
