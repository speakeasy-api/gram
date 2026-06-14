import {
  invalidateAllAiIntegrationConfig,
  useAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationConfig";
import { useDeleteAIIntegrationConfigMutation } from "@gram/client/react-query/deleteAIIntegrationConfig";
import { useUpsertAIIntegrationConfigMutation } from "@gram/client/react-query/upsertAIIntegrationConfig";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import type { AIIntegrationProvider } from "./ai-integration-providers";

type UseAIIntegrationConfigFormOptions = {
  onSaveSuccess?: () => void;
  onDeleteSuccess?: () => void;
};

export function useAIIntegrationConfigForm(
  provider: AIIntegrationProvider,
  options: UseAIIntegrationConfigFormOptions = {},
) {
  const { data, isLoading } = useAiIntegrationConfig({
    provider: provider.provider,
  });
  const queryClient = useQueryClient();

  const [enabled, setEnabled] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [organizationId, setOrganizationId] = useState("");

  const { mutate: upsert, status: upsertStatus } =
    useUpsertAIIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("AI integration saved");
        setApiKey("");
        void invalidateAllAiIntegrationConfig(queryClient);
        options.onSaveSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to save AI integration: ${err.message}`);
      },
    });
  const { mutate: deleteConfig, status: deleteStatus } =
    useDeleteAIIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("AI integration deleted");
        void invalidateAllAiIntegrationConfig(queryClient);
        options.onDeleteSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to delete AI integration: ${err.message}`);
      },
    });

  const isConfigured = Boolean(data?.id);
  const hasSavedKey = Boolean(data?.hasApiKey);

  // Sync form state from the persisted config. Depend on primitive values
  // rather than `data` itself: refetches produce new object references even
  // when nothing changed, and resetting on every refetch would discard
  // unsaved edits. The config id is included so that loading a *different*
  // config record (e.g. after the active organization changes) resets the
  // form even when the saved values happen to match the previous config.
  const hasData = Boolean(data);
  const savedId = data?.id ?? "";
  const savedEnabled = data?.enabled ?? false;
  const savedOrganizationId = data?.externalOrganizationId ?? "";

  useEffect(() => {
    if (!hasData) return;
    setEnabled(savedEnabled);
    setApiKey("");
    setOrganizationId(savedOrganizationId);
  }, [hasData, savedId, savedEnabled, savedOrganizationId]);

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";
  const canSave = useMemo(() => {
    if (isMutating) return false;
    if (apiKey.trim() === "" && !hasSavedKey) return false;
    if (provider.requiresOrganizationId && organizationId.trim() === "") {
      return false;
    }
    return true;
  }, [
    apiKey,
    hasSavedKey,
    isMutating,
    organizationId,
    provider.requiresOrganizationId,
  ]);

  const save = () => {
    upsert({
      request: {
        upsertAIIntegrationConfigRequest: {
          provider: provider.provider,
          apiKey: apiKey.trim(),
          enabled,
          ...(provider.requiresOrganizationId
            ? { externalOrganizationId: organizationId.trim() }
            : {}),
        },
      },
    });
  };

  const remove = () => {
    if (!isConfigured) return;
    deleteConfig({
      request: {
        deleteAIIntegrationConfigRequest: {
          provider: provider.provider,
        },
      },
    });
    setEnabled(false);
    setApiKey("");
    setOrganizationId("");
  };

  return {
    data,
    isLoading,
    enabled,
    setEnabled,
    apiKey,
    setApiKey,
    organizationId,
    setOrganizationId,
    isConfigured,
    hasSavedKey,
    isMutating,
    canSave,
    save,
    remove,
  };
}
