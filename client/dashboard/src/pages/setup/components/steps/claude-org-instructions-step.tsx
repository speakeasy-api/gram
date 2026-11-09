import { useMemo } from "react";
import { getCoworkTeamSteps } from "../../setup-data";
import { PlatformStepList } from "../platform-step-list";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";

interface ClaudeOrgInstructionsStepProps {
  onComplete: () => void;
}

export function ClaudeOrgInstructionsStep({
  onComplete,
}: ClaudeOrgInstructionsStepProps): JSX.Element {
  const { instruction, check } = useMemo(() => getCoworkTeamSteps(), []);

  return (
    <StepContainer
      title="Add Claude organization instructions"
      description="In Claude Desktop, every conversation starts in Chat, but Speakeasy's plugin only runs in Cowork. This instruction moves each conversation into Cowork from its first message."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <StepSection
          index={1}
          slug="add-org-instruction"
          title="Move every conversation into Cowork"
          description="This card completes on its own once Speakeasy receives the first Cowork conversation."
        >
          <PlatformStepList
            platformId="claude-cowork"
            steps={[instruction, check]}
          />
        </StepSection>

        <p className="text-muted-foreground text-sm">
          Policies aren't enforced in Cowork. For usage and cost, turn on Cowork
          monitoring in Set up Anthropic admin controls.
        </p>
      </div>
    </StepContainer>
  );
}
