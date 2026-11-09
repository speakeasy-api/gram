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
      description="In Claude Desktop, every conversation starts in Chat and moves into Cowork only when it needs to run something. Speakeasy's plugin runs only in Cowork, so a conversation that stays in Chat is never captured. This instruction has Claude run one harmless command before every reply, which moves each conversation into Cowork from its first message. The plugin then uploads the conversation each time Claude finishes a reply."
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
          Policies aren't enforced in Cowork, and uploaded conversations carry
          no token counts. Turn on Cowork's native monitoring in Set up
          Anthropic admin controls for usage and cost.
        </p>
      </div>
    </StepContainer>
  );
}
