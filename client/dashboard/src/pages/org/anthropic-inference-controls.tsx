import { useId, useState } from "react";
import { CodeBlock } from "@/components/code";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { AnthropicInferenceSetup } from "./use-anthropic-inference-setup";

// The two Speakeasy-side controls behind an inference hook: the webhook URL
// Claude posts to, and the signing secret that authenticates what arrives.
// Split rather than one component with a mode, because the guided setup card
// puts a walk through Claude.ai's own settings between them, while the
// integrations sheet stacks them together.

export function AnthropicInferenceWebhookURL({
  setup,
  /** Label for the action that mints the URL, when there isn't one yet. */
  prepareLabel = "Enable inference hooks",
}: {
  setup: AnthropicInferenceSetup;
  prepareLabel?: string;
}): JSX.Element {
  if (setup.webhookURL) {
    return <CodeBlock copyLabel="webhook URL">{setup.webhookURL}</CodeBlock>;
  }

  // A failed read can't tell "no endpoint yet" from "endpoint we couldn't
  // see", and minting on top of an existing one would move the URL out from
  // under a Claude org that already has it. So the action stays disabled and
  // the reader gets the read back rather than a dead button.
  if (setup.error != null) {
    return (
      <Stack gap={2} align="start">
        <Text small role="alert">
          Couldn&apos;t load your inference hook.
        </Text>
        <Button variant="secondary" onClick={setup.refetch}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }

  // The URL is minted by the first write, so an organization that has never
  // set this up has nothing to copy until it asks for one.
  return (
    <Stack gap={2} align="start">
      <Button
        disabled={setup.isPending || setup.busy}
        onClick={() => void setup.ensureConfig()}
      >
        <Button.Text>{setup.busy ? "Preparing…" : prepareLabel}</Button.Text>
      </Button>
      <Text small muted>
        One endpoint per organization. Speakeasy assigns the project and applies
        its security policies.
      </Text>
    </Stack>
  );
}

export function AnthropicInferenceSigningSecretForm({
  setup,
  /** Why the form can't be used yet, e.g. before the webhook URL exists. */
  heldBack,
}: {
  setup: AnthropicInferenceSetup;
  heldBack?: string;
}): JSX.Element {
  const fieldId = useId();
  const [signingSecret, setSigningSecret] = useState("");
  const hasSaved = setup.config?.hasSigningSecret ?? false;
  // With a secret already stored, saving again with the field blank is a way to
  // turn the hook on without rotating it.
  const canSave =
    !heldBack && !setup.busy && (Boolean(signingSecret.trim()) || hasSaved);

  const save = async () => {
    if (await setup.saveSecret(signingSecret)) setSigningSecret("");
  };

  return (
    <Stack gap={2}>
      <Label htmlFor={fieldId}>Signing secret</Label>
      <Input
        id={fieldId}
        type="password"
        autoComplete="new-password"
        placeholder={
          hasSaved ? "Saved — paste a new secret to rotate" : "whsec_…"
        }
        value={signingSecret}
        onChange={setSigningSecret}
        disabled={setup.busy || Boolean(heldBack)}
      />
      {hasSaved && (
        <Text small muted>
          Your secret is saved securely. Leave blank to keep it.
        </Text>
      )}
      {heldBack ? (
        <Text small muted>
          {heldBack}
        </Text>
      ) : null}
      <Stack direction="horizontal" align="center" gap={2}>
        <Button disabled={!canSave} onClick={() => void save()}>
          <Button.Text>
            {setup.saving ? "Saving…" : "Save signing secret"}
          </Button.Text>
        </Button>
      </Stack>
    </Stack>
  );
}
