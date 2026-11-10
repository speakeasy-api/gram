import { useState } from "react";
import { ConfirmTrafficSection } from "../confirm-traffic-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { StepContainer } from "../step-container";

export function ConfirmTrafficStep({
  onComplete,
}: {
  onComplete: () => void;
}): JSX.Element {
  const [confirmed, setConfirmed] = useState(false);

  return (
    <StepContainer
      title="Confirm traffic"
      description="We're listening for events from your agent platforms. Trigger any action in a managed coding agent to confirm the instrumentation works."
      onContinue={onComplete}
      canContinue={confirmed}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />
        <ConfirmTrafficSection
          index={2}
          description="Run a tool in a configured agent. Only events arriving after you open this task count toward confirmation."
          onConfirmed={setConfirmed}
        />
      </div>
    </StepContainer>
  );
}
