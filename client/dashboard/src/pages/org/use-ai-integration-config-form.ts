import {
  invalidateAllAiIntegrationConfig,
  useAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationConfig";
import { useDeleteAIIntegrationConfigMutation } from "@gram/client/react-query/deleteAIIntegrationConfig";
import { useUpsertAIIntegrationConfigMutation } from "@gram/client/react-query/upsertAIIntegrationConfig";
import { useQueryClient } from "@tanstack/react-query";
import {
  type Dispatch,
  type SetStateAction,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";
import type { AIIntegrationProvider } from "./ai-integration-providers";

type UseAIIntegrationConfigFormOptions = {
  onSaveSuccess?: () => void;
  onDeleteSuccess?: () => void;
};

type AIIntegrationConfigForm = {
  data: ReturnType<typeof useAiIntegrationConfig>["data"];
  isLoading: boolean;
  enabled: boolean;
  setEnabled: Dispatch<SetStateAction<boolean>>;
  apiKey: string;
  setApiKey: Dispatch<SetStateAction<string>>;
  organizationId: string;
  setOrganizationId: Dispatch<SetStateAction<string>>;
  isConfigured: boolean;
  hasSavedKey: boolean;
  isMutating: boolean;
  canSave: boolean;
  save: () => void;
  remove: () => void;
};

export function useAIIntegrationConfigForm(
  provider: AIIntegrationProvider,
  options: UseAIIntegrationConfigFormOptions = {},
): AIIntegrationConfigForm {
  const { data, isLoading } = useAiIntegrationConfig({
    provider: provider.provider,
  });
  const queryClient = useQueryClient();

  const [enabled, setEnabled] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [organizationId, setOrganizationId] = useState("");
  const lastSyncedConfigIdRef = useRef<string | null>(null);

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
        lastSyncedConfigIdRef.current = null;
        setEnabled(false);
        setApiKey("");
        setOrganizationId("");
        void invalidateAllAiIntegrationConfig(queryClient);
        options.onDeleteSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to delete AI integration: ${err.message}`);
      },
    });

  const isConfigured = Boolean(data?.id);
  const hasSavedKey = Boolean(data?.hasApiKey);

  useEffect(() => {
    if (!data?.id) {
      if (lastSyncedConfigIdRef.current === null) return;
      lastSyncedConfigIdRef.current = null;
      setEnabled(false);
      setApiKey("");
      setOrganizationId("");
      return;
    }

    if (lastSyncedConfigIdRef.current === data.id) return;
    lastSyncedConfigIdRef.current = data.id;
    setEnabled(data.enabled);
    setApiKey("");
    setOrganizationId(data.externalOrganizationId ?? "");
  }, [data]);

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
