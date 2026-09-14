import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useState } from "react";
import {
  AnthropicInferenceSigningSecretForm,
  AnthropicInferenceWebhookURL,
} from "./anthropic-inference-controls";
import { useAnthropicInferenceSetup } from "./use-anthropic-inference-setup";

export function AnthropicInferenceIntegrationRow(): JSX.Element {
  const setup = useAnthropicInferenceSetup();
  const [open, setOpen] = useState(false);
  const { config, connected, busy } = setup;

  const connect = async () => {
    // An organization that already has a webhook URL opens the sheet in the
    // same tick as the click; only a first connection waits on the mint.
    if (config?.id) {
      setOpen(true);
      return;
    }
    if (await setup.ensureConfig()) setOpen(true);
  };

  const disconnect = async () => {
    if (
      !window.confirm(
        "Disconnect Anthropic inference hooks? This revokes the webhook URL. Disable the hook in Claude too; its failure posture controls what happens when the endpoint is unavailable.",
      )
    )
      return;
    if (await setup.disconnect()) setOpen(false);
  };

  return (
    <div className="p-4">
      <Stack
        direction="horizontal"
        align="center"
        justify="space-between"
        gap={4}
      >
        <Stack gap={1} className="min-w-0">
          <Stack direction="horizontal" align="center" gap={2}>
            <AgentProviderIcon
              source={AGENT_PROVIDERS.claude.iconSource}
              className="h-4 w-4 shrink-0"
            />
            <Text className="font-medium">Anthropic inference hooks</Text>
            <Badge variant={connected ? "success" : "neutral"}>
              <Badge.Text>
                {setup.isPending
                  ? "Loading"
                  : setup.error
                    ? "Unavailable"
                    : connected
                      ? "Configured"
                      : config?.id
                        ? "Finish setup"
                        : "Not connected"}
              </Badge.Text>
            </Badge>
          </Stack>
          <Text muted small className="ml-6">
            Capture Claude conversations and enforce security policies before
            inference.
          </Text>
          {setup.error != null && (
            <Text role="alert" small>
              Unable to load this connection.{" "}
              <button className="underline" onClick={setup.refetch}>
                Retry
              </button>
            </Text>
          )}
        </Stack>
        <Button
          variant="secondary"
          size="sm"
          disabled={setup.isPending || Boolean(setup.error) || busy}
          onClick={() => void connect()}
        >
          <Button.Text>
            {connected ? "Configure" : config?.id ? "Finish setup" : "Connect"}
          </Button.Text>
        </Button>
      </Stack>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="right" className="overflow-y-auto sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>Anthropic inference hooks</SheetTitle>
            <SheetDescription>
              Connect once for your organization. Speakeasy assigns the project
              automatically and uses its security policies.
            </SheetDescription>
          </SheetHeader>
          <Stack gap={6} className="px-4">
            <Stack gap={2}>
              <Text className="font-medium">1. Add the webhook in Claude</Text>
              <Text small>
                In Claude’s Organization settings → Data and privacy → Inference
                hooks, paste this URL. Test the connection and save with Enforce
                verdicts off.
              </Text>
              <AnthropicInferenceWebhookURL setup={setup} />
              <a
                className="text-sm underline"
                href="https://platform.claude.com/docs/en/manage-claude/inference-hooks-configuration"
                target="_blank"
                rel="noreferrer"
              >
                Claude setup instructions ↗
              </a>
            </Stack>
            <Stack gap={2}>
              <Text className="font-medium">
                2. Save Claude’s signing secret
              </Text>
              <Text small>
                Claude reveals the secret after you save. Paste it here to
                authenticate deliveries.
              </Text>
              <AnthropicInferenceSigningSecretForm setup={setup} />
            </Stack>
            <Stack gap={2}>
              <Text className="font-medium">
                3. Enable protection in Claude
              </Text>
              <Text small>
                Turn on Enforce verdicts, set Failure posture to Block and the
                timeout to 10 seconds, then save. Claude now sends conversation
                history to Speakeasy for policy checks before inference.
              </Text>
            </Stack>
          </Stack>
          <SheetFooter>
            {config?.id && (
              <Button
                variant="destructive-secondary"
                disabled={busy}
                onClick={() => void disconnect()}
              >
                <Button.Text>Disconnect</Button.Text>
              </Button>
            )}
            <Button variant="secondary" onClick={() => setOpen(false)}>
              <Button.Text>Done</Button.Text>
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  );
}
