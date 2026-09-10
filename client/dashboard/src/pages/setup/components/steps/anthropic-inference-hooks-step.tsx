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

interface AnthropicInferenceHooksStepProps {
  onComplete: () => void;
}

// Inference hooks are configured entirely from Claude.ai's own admin settings,
// with one value going each way: Speakeasy mints the endpoint Claude posts to,
// and Claude reveals the secret that signs what it sends. The card alternates
// between the two products in that order rather than grouping the Speakeasy
// controls together, because neither value exists until the other side asks
// for it.
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
      description="Anthropic inference hooks send every Claude conversation to an endpoint of your choosing before the model sees it. Point them at Speakeasy and ordinary Claude.ai conversations — not just the ones a coding agent has — become sessions you can read, police and account for. Turn the hook on, sign it, and confirm conversations arrive."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        <EnableLoggingSection index={1} />

        <StepSection
          index={2}
          slug="enable-inference-hooks"
          title="Enable inference hooks in Speakeasy"
          description="Speakeasy mints one endpoint for your organization. Claude posts every conversation to it, and Speakeasy assigns the project and applies its security policies automatically."
          complete={prepared}
        >
          <div className="space-y-3">
            <AnthropicInferenceWebhookURL setup={setup} />
            {prepared ? (
              <p className="text-muted-foreground text-sm">
                Copy this URL — the next step pastes it into Claude.
              </p>
            ) : null}
          </div>
        </StepSection>

        <StepSection
          index={3}
          slug="add-endpoint-in-claude"
          title="Add the endpoint in Claude.ai"
          description="Claude keeps inference hooks in its own admin settings, under Data and privacy."
        >
          <div className="space-y-3">
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
              as a Claude organization admin — Admin settings → Data and privacy
              → Inference hooks. Click <strong>Edit</strong> on{" "}
              <strong>Inference hooks endpoint</strong> and paste the URL from
              the previous step.
            </p>
            {prepared ? null : (
              <p className="text-muted-foreground text-sm">
                Generate the webhook URL in the previous step first — there is
                nothing to paste yet.
              </p>
            )}
          </div>
        </StepSection>

        <StepSection
          index={4}
          slug="turn-on-inference-hooks"
          title="Turn on inference hooks"
          description="Turning Enforce verdicts on is what starts inspection: with it off, Claude never calls the endpoint at all."
        >
          <div className="space-y-4">
            <p className="text-muted-foreground text-sm leading-relaxed">
              Switch <strong>Enforce verdicts</strong> on. The
              &ldquo;Enforcement is off&rdquo; banner clears and{" "}
              <strong>Endpoint status</strong> starts reporting.
            </p>
            <Alert variant="info" alignTop>
              <AlertTitle>Begin your rollout in Shadow mode</AlertTitle>
              <AlertDescription>
                Under Failure handling, set <strong>Mode</strong> to{" "}
                <strong>Shadow mode</strong>. Claude still calls the endpoint
                and records every verdict, but always lets the request through,
                so a misconfigured endpoint can&apos;t block your organization
                from using Claude. Switch to <strong>Block the request</strong>{" "}
                once Endpoint status reads Healthy.
              </AlertDescription>
            </Alert>
            <p className="text-muted-foreground text-sm leading-relaxed">
              To ramp more gradually still, lower{" "}
              <strong>Requests inspected (%)</strong> under Rollout and raise it
              as the endpoint proves itself. Each request rolls once for its
              whole conversation turn, so a partial rollout gives you whole
              conversations rather than fragments of every one.
            </p>
          </div>
        </StepSection>

        <StepSection
          index={5}
          slug="save-signing-secret"
          title="Save the signing secret"
          description="Speakeasy authenticates each delivery against this secret, so an endpoint URL on its own isn't enough to write conversations into your organization."
          complete={setup.connected}
        >
          <div className="space-y-4">
            <p className="text-muted-foreground text-sm leading-relaxed">
              Still in Claude&apos;s inference hooks settings, find{" "}
              <strong>Request signing</strong> and generate the signing secret —
              the button reads <strong>Rotate secret</strong> if your
              organization already has one. Claude shows the{" "}
              <code className="text-foreground">whsec_…</code> value once, so
              paste it here before leaving the page.
            </p>
            <AnthropicInferenceSigningSecretForm
              setup={setup}
              heldBack={
                prepared
                  ? undefined
                  : "Generate the webhook URL in step 2 first — there is no hook to sign yet."
              }
            />
          </div>
        </StepSection>

        <ConfirmInferenceTrafficSection
          index={6}
          description="Send a message in Claude.ai. The conversation reaches Speakeasy before the model answers, and shows up here."
          callout={{
            title: "Conversations, not tool calls",
            body: "An inference hook delivers the conversation Claude is about to answer, so a plain question is enough — no tool call, no plugin, and nothing installed on anyone's machine. Only conversations started after enforcement was switched on are sent.",
          }}
        />
      </div>
    </StepContainer>
  );
}
