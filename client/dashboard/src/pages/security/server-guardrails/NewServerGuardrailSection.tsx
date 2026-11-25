import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useDetectorMode } from "../use-detector-mode";
import { ServerGuardrailsForm } from "./ServerGuardrailsForm";
import type { NewServerGuardrail } from "./useNewServerGuardrail";

/** The Guardrails section of a create-server form: off by default, and the full
 *  form only once the user opts in. Renders nothing when guardrails are not
 *  available to this user. */
export function NewServerGuardrailSection({
  guardrail,
  serverName,
  disabled,
}: {
  guardrail: NewServerGuardrail;
  serverName: string;
  disabled?: boolean;
}): JSX.Element | null {
  if (!guardrail.available) return null;
  return (
    <NewServerGuardrailBody
      guardrail={guardrail}
      serverName={serverName}
      disabled={disabled}
    />
  );
}

function NewServerGuardrailBody({
  guardrail,
  serverName,
  disabled,
}: {
  guardrail: NewServerGuardrail;
  serverName: string;
  disabled?: boolean;
}): JSX.Element {
  const mode = useDetectorMode();
  return (
    <div className="border-border space-y-4 border p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-sm font-medium">Guardrails</h3>
          <Text small muted>
            Inspect traffic through {serverName} for secrets, sensitive data and
            risky tool calls. The policy is scoped to this server only, and you
            can change it later under its Guardrails tab.
          </Text>
        </div>
        <Switch
          aria-label="Add a guardrail for this server"
          checked={guardrail.enabled}
          disabled={disabled}
          onCheckedChange={guardrail.setEnabled}
        />
      </div>
      {guardrail.enabled ? (
        <fieldset disabled={disabled} className="contents">
          <ServerGuardrailsForm
            state={guardrail.state}
            onChange={guardrail.updateState}
            toolsSource={{ status: "unavailable" }}
            serverName={serverName}
            mode={mode}
          />
        </fieldset>
      ) : null}
      {guardrail.validation.ok ? null : (
        <Text small className="text-destructive" role="alert">
          {guardrail.validation.message}
        </Text>
      )}
    </div>
  );
}
