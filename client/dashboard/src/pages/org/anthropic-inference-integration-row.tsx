import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { CodeBlock } from "@/components/code";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
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
import { getServerURL } from "@/lib/utils";
import {
  invalidateAllAnthropicInferenceConfig,
  useAnthropicInferenceConfig,
} from "@gram/client/react-query/anthropicInferenceConfig";
import { useUpsertAnthropicInferenceConfigMutation } from "@gram/client/react-query/upsertAnthropicInferenceConfig";
import { useDeleteAnthropicInferenceConfigMutation } from "@gram/client/react-query/deleteAnthropicInferenceConfig";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

export function AnthropicInferenceIntegrationRow(): JSX.Element {
  const queryClient = useQueryClient();
  const query = useAnthropicInferenceConfig(undefined, undefined, {
    throwOnError: false,
  });
  const [open, setOpen] = useState(false);
  const [signingSecret, setSigningSecret] = useState("");
  const upsert = useUpsertAnthropicInferenceConfigMutation({ gcTime: 0 });
  const remove = useDeleteAnthropicInferenceConfigMutation();
  const config = query.data;
  const connected = Boolean(config?.enabled && config.hasSigningSecret);
  const busy = upsert.isPending || remove.isPending;
  const webhookURL = config?.webhookPath
    ? new URL(config.webhookPath, getServerURL()).toString()
    : "";

  const setSheetOpen = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setSigningSecret("");
      upsert.reset();
    }
  };

  const connect = async () => {
    if (!config?.id) {
      try {
        await upsert.mutateAsync({
          request: {
            upsertAnthropicInferenceConfigRequestBody: { enabled: false },
          },
        });
        await invalidateAllAnthropicInferenceConfig(queryClient);
      } catch {
        toast.error("Unable to prepare Anthropic inference hooks");
        upsert.reset();
        return;
      }
    }
    setOpen(true);
  };

  const save = async () => {
    try {
      await upsert.mutateAsync({
        request: {
          upsertAnthropicInferenceConfigRequestBody: {
            signingSecret: signingSecret.trim() || undefined,
            enabled: true,
          },
        },
      });
      setSigningSecret("");
      upsert.reset();
      await invalidateAllAnthropicInferenceConfig(queryClient);
      toast.success(
        "Signing secret saved. Enable Enforce verdicts in Claude to activate protection.",
      );
    } catch {
      toast.error(
        "Unable to save. Check the Anthropic signing secret and try again.",
      );
      upsert.reset();
    }
  };

  const disconnect = async () => {
    if (
      !window.confirm(
        "Disconnect Anthropic inference hooks? This revokes the webhook URL. Disable the hook in Claude too; its failure posture controls what happens when the endpoint is unavailable.",
      )
    )
      return;
    try {
      await remove.mutateAsync({ request: {} });
      await invalidateAllAnthropicInferenceConfig(queryClient);
      setSheetOpen(false);
      toast.success("Anthropic inference hooks disconnected");
    } catch {
      toast.error("Unable to disconnect Anthropic inference hooks");
    }
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
                {query.isPending
                  ? "Loading"
                  : query.error
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
          {query.error && (
            <Text role="alert" small>
              Unable to load this connection.{" "}
              <button
                className="underline"
                onClick={() => void query.refetch()}
              >
                Retry
              </button>
            </Text>
          )}
        </Stack>
        <Button
          variant="secondary"
          size="sm"
          disabled={query.isPending || Boolean(query.error) || busy}
          onClick={() => void connect()}
        >
          <Button.Text>
            {connected ? "Configure" : config?.id ? "Finish setup" : "Connect"}
          </Button.Text>
        </Button>
      </Stack>
      <Sheet open={open} onOpenChange={setSheetOpen}>
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
              {webhookURL ? (
                <CodeBlock copyLabel="webhook URL">{webhookURL}</CodeBlock>
              ) : (
                <Text small>Loading webhook URL…</Text>
              )}
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
              <Label htmlFor="anthropic-signing-secret">Signing secret</Label>
              <Input
                id="anthropic-signing-secret"
                type="password"
                autoComplete="new-password"
                placeholder={
                  config?.hasSigningSecret
                    ? "Saved — paste a new secret to rotate"
                    : "whsec_…"
                }
                value={signingSecret}
                onChange={setSigningSecret}
                disabled={busy}
              />
              {config?.hasSigningSecret && (
                <Text small muted>
                  Your secret is saved securely. Leave blank to keep it.
                </Text>
              )}
              <Button
                disabled={
                  busy || (!signingSecret.trim() && !config?.hasSigningSecret)
                }
                onClick={() => void save()}
              >
                <Button.Text>
                  {upsert.isPending ? "Saving…" : "Save signing secret"}
                </Button.Text>
              </Button>
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
            <Button variant="secondary" onClick={() => setSheetOpen(false)}>
              <Button.Text>Done</Button.Text>
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  );
}
