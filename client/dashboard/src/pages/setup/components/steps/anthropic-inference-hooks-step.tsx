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

interface AnthropicInferenceHooksStepProps {
  onComplete: () => void;
}

// Inference hooks are configured entirely from Claude.ai's own admin settings,
// with one value going each way: Speakeasy mints the endpoint Claude posts to,
// and Claude reveals the secret that signs what it sends.
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
      description="Anthropic inference hooks send every Claude conversation to an endpoint of your choosing before the model sees it. Point them at Speakeasy and ordinary Claude.ai conversations — not just the ones a coding agent has — become sessions you can read, police and account for. Nothing is installed on anyone's machine."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <StepSection
          index={2}
          slug="turn-on-inference-hooks"
          title="Turn on inference hooks"
          description="Claude keeps inference hooks in its own admin settings, under Data and privacy. One pass through that page does all of this: paste the endpoint Speakeasy mints, switch inspection on, and bring back the secret that signs each delivery."
          complete={setup.connected}
        >
          <div className="space-y-6">
            <Instruction number={1} title="Copy your endpoint">
              <p className="text-muted-foreground text-sm leading-relaxed">
                Speakeasy mints one endpoint for your organization. Claude posts
                every conversation to it, and Speakeasy assigns the project and
                applies its security policies automatically.
              </p>
              <AnthropicInferenceWebhookURL setup={setup} />
            </Instruction>

            <Instruction number={2} title="Add it in Claude.ai">
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
                as a Claude organization admin — Admin settings → Data and
                privacy → Inference hooks. Click <strong>Edit</strong> on{" "}
                <strong>Inference hooks endpoint</strong> and paste the URL you
                just copied.
              </p>
            </Instruction>

            <Instruction number={3} title="Switch Enforce verdicts on">
              <p className="text-muted-foreground text-sm leading-relaxed">
                This is what starts inspection: with it off, Claude never calls
                the endpoint at all. The &ldquo;Enforcement is off&rdquo; banner
                clears and <strong>Endpoint status</strong> starts reporting.
              </p>
              <Alert variant="info" alignTop>
                <AlertTitle>Begin your rollout in Shadow mode</AlertTitle>
                <AlertDescription>
                  Under Failure handling, set <strong>Mode</strong> to{" "}
                  <strong>Shadow mode</strong>. Claude still calls the endpoint
                  and records every verdict, but always lets the request
                  through, so a misconfigured endpoint can&apos;t block your
                  organization from using Claude. Switch to{" "}
                  <strong>Block the request</strong> once Endpoint status reads
                  Healthy. To ramp more gradually still, lower{" "}
                  <strong>Requests inspected (%)</strong> under Rollout — each
                  request rolls once for its whole conversation turn, so a
                  partial rollout gives you whole conversations rather than
                  fragments of every one.
                </AlertDescription>
              </Alert>
            </Instruction>

            <Instruction number={4} title="Save the signing secret">
              <p className="text-muted-foreground text-sm leading-relaxed">
                Still on that page, find <strong>Request signing</strong> and
                generate the signing secret — the button reads{" "}
                <strong>Rotate secret</strong> if your organization already has
                one. Claude shows the{" "}
                <code className="text-foreground">whsec_…</code> value once, so
                paste it here before leaving the page. Speakeasy authenticates
                each delivery against it, so the endpoint URL on its own
                isn&apos;t enough to write conversations into your organization.
              </p>
              <AnthropicInferenceSigningSecretForm
                setup={setup}
                heldBack={
                  prepared
                    ? undefined
                    : "Copy your endpoint above first — there is no hook to sign yet."
                }
              />
            </Instruction>
          </div>
        </StepSection>

        <ConfirmInferenceTrafficSection
          index={3}
          description="Send any message in Claude. The hook delivers the conversation before the model answers it, so a plain question is enough — no tool call, nothing to install."
        />
      </div>
    </StepContainer>
  );
}
