import {
  invalidateAllAiIntegrationConfig,
  useAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationConfig";
import { useDeleteAIIntegrationConfigMutation } from "@gram/client/react-query/deleteAIIntegrationConfig";
import { invalidateAllAiIntegrationOrgUnits } from "@gram/client/react-query/aiIntegrationOrgUnits";
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

export type AIIntegrationConfigForm = {
  data: ReturnType<typeof useAiIntegrationConfig>["data"];
  isLoading: boolean;
  enabled: boolean;
  setEnabled: Dispatch<SetStateAction<boolean>>;
  apiKey: string;
  setApiKey: Dispatch<SetStateAction<string>>;
  organizationId: string;
  setOrganizationId: Dispatch<SetStateAction<string>>;
  delegatedUser: string;
  setDelegatedUser: Dispatch<SetStateAction<string>>;
  orgUnitId: string;
  setOrgUnitId: Dispatch<SetStateAction<string>>;
  // Saved, but syncing waits for an org unit (gemini_compliance step 2).
  awaitingOrgUnit: boolean;
  billingMode: string;
  setBillingMode: Dispatch<SetStateAction<string>>;
  isConfigured: boolean;
  hasSavedKey: boolean;
  // The saved (not the edited) delegated admin; listing org units needs it.
  hasSavedDelegatedUser: boolean;
  isMutating: boolean;
  canSave: boolean;
  save: () => void;
  saveEnabled: (nextEnabled: boolean) => void;
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
  const [delegatedUser, setDelegatedUser] = useState("");
  const [orgUnitId, setOrgUnitId] = useState("");
  const [billingMode, setBillingMode] = useState("unknown");
  const lastSyncedConfigIdRef = useRef<string | null>(null);

  const { mutate: upsert, status: upsertStatus } =
    useUpsertAIIntegrationConfigMutation({
      onSuccess: (saved) => {
        setApiKey("");
        void invalidateAllAiIntegrationConfig(queryClient);
        if (provider.selectsOrgUnit) {
          // The unit list depends on the saved key and delegated admin.
          void invalidateAllAiIntegrationOrgUnits(queryClient);
          if (!saved.orgUnitId) {
            // Keep the form open: the next step is choosing what to export.
            toast.success(
              "Credentials saved. Choose an organizational unit to start syncing.",
            );
            return;
          }
        }
        toast.success("AI integration saved");
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
        setDelegatedUser("");
        setOrgUnitId("");
        setBillingMode("unknown");
        void invalidateAllAiIntegrationConfig(queryClient);
        options.onDeleteSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to delete AI integration: ${err.message}`);
      },
    });

  const isConfigured = Boolean(data?.id);
  const hasSavedKey = Boolean(data?.hasApiKey);
  const hasSavedDelegatedUser = Boolean(data?.delegatedUser);
  const awaitingOrgUnit = Boolean(
    provider.selectsOrgUnit && isConfigured && !data?.orgUnitId,
  );

  useEffect(() => {
    if (!data?.id) {
      if (lastSyncedConfigIdRef.current === null) return;
      lastSyncedConfigIdRef.current = null;
      setEnabled(false);
      setApiKey("");
      setOrganizationId("");
      setDelegatedUser("");
      setOrgUnitId("");
      setBillingMode("unknown");
      return;
    }

    if (lastSyncedConfigIdRef.current === data.id) return;
    lastSyncedConfigIdRef.current = data.id;
    setEnabled(data.enabled);
    setApiKey("");
    setOrganizationId(data.externalOrganizationId ?? "");
    setDelegatedUser(data.delegatedUser ?? "");
    setOrgUnitId(data.orgUnitId ?? "");
    setBillingMode(data.billingMode ?? "unknown");
  }, [data]);

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";
  const canSave = useMemo(() => {
    if (isMutating) return false;
    if (apiKey.trim() === "" && !hasSavedKey) return false;
    if (provider.requiresOrganizationId && organizationId.trim() === "") {
      return false;
    }
    if (provider.delegatedUser && delegatedUser.trim() === "") return false;
    if (awaitingOrgUnit && orgUnitId === "") return false;
    return true;
  }, [
    apiKey,
    awaitingOrgUnit,
    delegatedUser,
    hasSavedKey,
    isMutating,
    orgUnitId,
    organizationId,
    provider.delegatedUser,
    provider.requiresOrganizationId,
  ]);

  const save = () => {
    upsert({
      request: {
        upsertConfigRequestBody: {
          provider: provider.provider,
          apiKey: apiKey.trim(),
          // A first-time connect starts enabled, as does choosing the first
          // org unit for a connection held until one is picked; the
          // connection switch and dialog manage the flag from then on.
          enabled: isConfigured && !awaitingOrgUnit ? enabled : true,
          billingMode,
          ...(provider.requiresOrganizationId
            ? { externalOrganizationId: organizationId.trim() }
            : {}),
          ...(provider.delegatedUser
            ? { delegatedUser: delegatedUser.trim() }
            : {}),
          ...(provider.selectsOrgUnit && orgUnitId !== "" ? { orgUnitId } : {}),
        },
      },
    });
  };

  // Instantly persists an enabled/disabled flip for an already-saved
  // connection, keeping the rest of the stored config as-is. Lets the
  // connection switch act on its own instead of waiting for the Save button.
  const saveEnabled = (nextEnabled: boolean) => {
    setEnabled(nextEnabled);
    if (!isConfigured || !hasSavedKey) return;
    upsert({
      request: {
        upsertConfigRequestBody: {
          provider: provider.provider,
          apiKey: "",
          enabled: nextEnabled,
          billingMode,
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
        deleteConfigRequestBody: {
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
    delegatedUser,
    setDelegatedUser,
    orgUnitId,
    setOrgUnitId,
    awaitingOrgUnit,
    billingMode,
    setBillingMode,
    isConfigured,
    hasSavedKey,
    hasSavedDelegatedUser,
    isMutating,
    canSave,
    save,
    saveEnabled,
    remove,
  };
}
