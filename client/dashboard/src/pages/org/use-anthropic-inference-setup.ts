import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";
import type { AnthropicInferenceConfig } from "@gram/client/models/components/anthropicinferenceconfig.js";
import {
  invalidateAllAnthropicInferenceConfig,
  useAnthropicInferenceConfig,
} from "@gram/client/react-query/anthropicInferenceConfig";
import { useDeleteAnthropicInferenceConfigMutation } from "@gram/client/react-query/deleteAnthropicInferenceConfig";
import { useUpsertAnthropicInferenceConfigMutation } from "@gram/client/react-query/upsertAnthropicInferenceConfig";
import { getServerURL } from "@/lib/utils";

export interface AnthropicInferenceSetup {
  config: AnthropicInferenceConfig | undefined;
  /** Absolute URL Claude posts deliveries to. Empty until the config exists. */
  webhookURL: string;
  /** The hook is on in Speakeasy and can authenticate what Claude sends. */
  connected: boolean;
  /** The organization's config hasn't been read yet. */
  isPending: boolean;
  error: unknown;
  refetch: () => void;
  /** A write is in flight, so every control that starts one is disabled. */
  busy: boolean;
  saving: boolean;
  /** Mints the webhook path if the org has no config yet. */
  ensureConfig: () => Promise<boolean>;
  /** Stores the secret Claude revealed and turns the hook on. */
  saveSecret: (secret: string) => Promise<boolean>;
  /** Revokes the webhook URL. */
  disconnect: () => Promise<boolean>;
}

// The organization's inference hook config, plus the three writes that move it
// between states. Shared so the integrations sheet and the setup card drive one
// set of semantics — which write mints the webhook path, what a save means,
// what each failure says — rather than each re-deriving them from the mutations.
export function useAnthropicInferenceSetup(): AnthropicInferenceSetup {
  const queryClient = useQueryClient();
  const query = useAnthropicInferenceConfig(undefined, undefined, {
    throwOnError: false,
  });
  const upsert = useUpsertAnthropicInferenceConfigMutation({ gcTime: 0 });
  const remove = useDeleteAnthropicInferenceConfigMutation();

  const config = query.data;
  const configID = config?.id;
  const upsertAsync = upsert.mutateAsync;
  const removeAsync = remove.mutateAsync;
  const resetUpsert = upsert.reset;

  const ensureConfig = useCallback(async () => {
    // A config with an id already has its webhook path; minting a second one
    // would move the URL out from under a Claude org that has the first.
    if (configID) return true;
    try {
      await upsertAsync({
        request: {
          upsertAnthropicInferenceConfigRequestBody: { enabled: false },
        },
      });
      await invalidateAllAnthropicInferenceConfig(queryClient);
      return true;
    } catch {
      toast.error("Unable to prepare Anthropic inference hooks");
      resetUpsert();
      return false;
    }
  }, [configID, queryClient, resetUpsert, upsertAsync]);

  const saveSecret = useCallback(
    async (secret: string) => {
      try {
        await upsertAsync({
          request: {
            upsertAnthropicInferenceConfigRequestBody: {
              signingSecret: secret.trim() || undefined,
              enabled: true,
            },
          },
        });
        resetUpsert();
        await invalidateAllAnthropicInferenceConfig(queryClient);
        toast.success(
          "Signing secret saved. Enable Enforce verdicts in Claude to activate protection.",
        );
        return true;
      } catch {
        toast.error(
          "Unable to save. Check the Anthropic signing secret and try again.",
        );
        resetUpsert();
        return false;
      }
    },
    [queryClient, resetUpsert, upsertAsync],
  );

  const disconnect = useCallback(async () => {
    try {
      await removeAsync({ request: {} });
      await invalidateAllAnthropicInferenceConfig(queryClient);
      toast.success("Anthropic inference hooks disconnected");
      return true;
    } catch {
      toast.error("Unable to disconnect Anthropic inference hooks");
      return false;
    }
  }, [queryClient, removeAsync]);

  return {
    config,
    webhookURL: config?.webhookPath
      ? new URL(config.webhookPath, getServerURL()).toString()
      : "",
    connected: Boolean(config?.enabled && config.hasSigningSecret),
    isPending: query.isPending,
    error: query.error,
    refetch: () => void query.refetch(),
    busy: upsert.isPending || remove.isPending,
    saving: upsert.isPending,
    ensureConfig,
    saveSecret,
    disconnect,
  };
}
