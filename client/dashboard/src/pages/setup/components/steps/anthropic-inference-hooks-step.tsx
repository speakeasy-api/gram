import type { ReactNode } from "react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Link } from "@/components/ui/Link";
import {
  AnthropicInferenceSigningSecretForm,
  AnthropicInferenceWebhookURL,
} from "@/pages/org/anthropic-inference-controls";
import { useAnthropicInferenceSetup } from "@/pages/org/use-anthropic-inference-setup";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { EnableLoggingSection } from "../enable-logging-section";
import { ConfirmInferenceTrafficSection } from "../confirm-inference-traffic-section";

const INFERENCE_HOOKS_SETTINGS_URL =
  "https://claude.ai/admin-settings/inference-hooks";

// One instruction inside the step. These are not rail entries: turning the
// hook on is a single pass through one Claude.ai settings page, and splitting
// it across steps would make the reader leave and re-enter that page four
// times for what is really one sitting.
function Instruction({
  number,
  title,
  children,
}: {
  number: number;
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <h4 className="text-foreground text-sm font-semibold">
        {number}. {title}
      </h4>
      {children}
    </div>
  );
}

// The two rows on Claude's page that are hard to pick out. The rest of the
// settings are named in the prose and need no picture.
function Shot({ src, alt }: { src: string; alt: string }): JSX.Element {
  return (
    <figure className="border-border overflow-hidden border">
      <img src={src} alt={alt} className="w-full" />
    </figure>
  );
}

interface AnthropicInferenceHooksStepProps {
  onComplete: () => void;
}

export function AnthropicInferenceHooksStep({
  onComplete,
}: AnthropicInferenceHooksStepProps): JSX.Element {
  const setup = useAnthropicInferenceSetup();
  const prepared = Boolean(setup.config?.id);

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic observability"
      description="Anthropic inference hooks send every Claude conversation to Speakeasy before the model answers it. Nothing is installed on anyone's machine."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <StepSection
          index={2}
          slug="enable-inference-hooks"
          title="Enable inference hooks"
          description="One pass through Claude's Admin settings → Data and privacy → Inference hooks."
          complete={setup.connected}
        >
          <div className="space-y-6">
            <Instruction number={1} title="Generate your endpoint">
              <AnthropicInferenceWebhookURL
                setup={setup}
                prepareLabel="Generate endpoint"
              />
            </Instruction>

            <Instruction number={2} title="Paste it into Claude">
              <p className="text-muted-foreground text-sm leading-relaxed">
                Open{" "}
                <Link
                  href={INFERENCE_HOOKS_SETTINGS_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  size="sm"
                  iconSuffixName="external-link"
                >
                  Inference hooks
                </Link>{" "}
                as a Claude organization admin. Click <strong>Edit</strong> and
                paste the URL.
              </p>
              <Shot
                src="/setup/claude-inference-hooks-endpoint.png"
                alt="The Inference hooks endpoint row in Claude's admin settings, with its Edit button"
              />
            </Instruction>

            <Instruction number={3} title="Switch Enforce verdicts on">
              <p className="text-muted-foreground text-sm leading-relaxed">
                Claude doesn&apos;t call your endpoint until this is on.
              </p>
              <Alert variant="info" alignTop>
                <AlertTitle>Begin in Shadow mode</AlertTitle>
                <AlertDescription>
                  Set Failure handling → <strong>Mode</strong> to{" "}
                  <strong>Shadow mode</strong>. Claude records verdicts without
                  blocking, so you can tune your policies before enforcing them.
                </AlertDescription>
              </Alert>
            </Instruction>

            <Instruction number={4} title="Save the signing secret">
              <p className="text-muted-foreground text-sm leading-relaxed">
                Under <strong>Request signing</strong>, generate the secret.
                Claude shows it once.
              </p>
              <Shot
                src="/setup/claude-inference-hooks-signing-secret.png"
                alt="The Request signing row in Claude's admin settings, with its Rotate secret button"
              />
              <AnthropicInferenceSigningSecretForm
                setup={setup}
                heldBack={
                  prepared ? undefined : "Generate your endpoint above first."
                }
              />
            </Instruction>
          </div>
        </StepSection>

        <ConfirmInferenceTrafficSection
          index={3}
          description="Send any message in Claude. The hook delivers the conversation before the model answers it."
        />
      </div>
    </StepContainer>
  );
}
