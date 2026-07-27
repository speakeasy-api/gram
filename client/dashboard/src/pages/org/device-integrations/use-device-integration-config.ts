import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { useDeleteDeviceIntegrationConfigMutation } from "@gram/client/react-query/deleteDeviceIntegrationConfig.js";
import {
  invalidateAllDeviceIntegrationConfig,
  useDeviceIntegrationConfig as useDeviceIntegrationConfigQuery,
} from "@gram/client/react-query/deviceIntegrationConfig.js";
import { invalidateAllDeviceIntegrationSchedules } from "@gram/client/react-query/deviceIntegrationSchedules.js";
import { useTestDeviceIntegrationConnectionMutation } from "@gram/client/react-query/testDeviceIntegrationConnection.js";
import { useUpsertDeviceIntegrationConfigMutation } from "@gram/client/react-query/upsertDeviceIntegrationConfig.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

type FieldValues = Record<string, string>;

export type TestConnectionResult = {
  ok: boolean;
  message: string | null;
};

type UseDeviceIntegrationConfigOptions = {
  onSaveSuccess?: () => void;
  onDeleteSuccess?: () => void;
};

export type DeviceIntegrationConfigForm = {
  isLoading: boolean;
  enabled: boolean;
  isConfigured: boolean;
  hasSavedCredentials: boolean;
  // Secret field values being typed this session. Saved secrets are never
  // present here — the API is write-only for credentials.
  credentials: FieldValues;
  setCredential: (key: string, value: string) => void;
  settings: FieldValues;
  setSetting: (key: string, value: string) => void;
  isMutating: boolean;
  canSave: boolean;
  save: () => void;
  saveEnabled: (nextEnabled: boolean) => void;
  remove: () => void;
  testConnection: () => void;
  isTesting: boolean;
  testResult: TestConnectionResult | null;
  clearTestResult: () => void;
};

// Form state for one provider's org-wide config, driven entirely by the
// provider descriptor's field spec: secret fields collect into the write-only
// credentials document, non-secret fields into readable settings.
export function useDeviceIntegrationConfigForm(
  provider: DeviceIntegrationProvider,
  options: UseDeviceIntegrationConfigOptions = {},
): DeviceIntegrationConfigForm {
  const { data, isLoading } = useDeviceIntegrationConfigQuery({
    provider: provider.id,
  });
  const queryClient = useQueryClient();

  const [enabled, setEnabled] = useState(false);
  const [credentials, setCredentials] = useState<FieldValues>({});
  const [settings, setSettings] = useState<FieldValues>({});
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(
    null,
  );
  const lastSyncedConfigIdRef = useRef<string | null>(null);

  const invalidate = () => {
    void invalidateAllDeviceIntegrationConfig(queryClient);
    void invalidateAllDeviceIntegrationSchedules(queryClient);
  };

  const { mutate: upsert, status: upsertStatus } =
    useUpsertDeviceIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("Device integration saved");
        setCredentials({});
        setTestResult(null);
        invalidate();
        options.onSaveSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to save device integration: ${err.message}`);
      },
    });
  const { mutate: deleteConfig, status: deleteStatus } =
    useDeleteDeviceIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("Device integration deleted");
        lastSyncedConfigIdRef.current = null;
        setEnabled(false);
        setCredentials({});
        setSettings({});
        setTestResult(null);
        invalidate();
        options.onDeleteSuccess?.();
      },
      onError: (err) => {
        toast.error(`Failed to delete device integration: ${err.message}`);
      },
    });
  const { mutate: runTest, status: testStatus } =
    useTestDeviceIntegrationConnectionMutation({
      onSuccess: (result) => {
        setTestResult({ ok: result.ok, message: result.message ?? null });
      },
      onError: (err) => {
        setTestResult({ ok: false, message: err.message });
      },
    });

  const isConfigured = Boolean(data?.id);
  const hasSavedCredentials = Boolean(data?.hasCredentials);

  useEffect(() => {
    if (!data?.id) {
      if (lastSyncedConfigIdRef.current === null) return;
      lastSyncedConfigIdRef.current = null;
      setEnabled(false);
      setCredentials({});
      setSettings({});
      return;
    }

    if (lastSyncedConfigIdRef.current === data.id) return;
    lastSyncedConfigIdRef.current = data.id;
    setEnabled(data.enabled);
    setCredentials({});
    setSettings(data.settings);
  }, [data]);

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";

  const canSave = useMemo(() => {
    if (isMutating) return false;
    for (const field of provider.fields) {
      const value = field.secret
        ? (credentials[field.key] ?? "")
        : (settings[field.key] ?? "");
      if (!field.required || value.trim() !== "") continue;
      // A saved secret satisfies its requirement without being retyped.
      if (field.secret && hasSavedCredentials) continue;
      return false;
    }
    return true;
  }, [credentials, hasSavedCredentials, isMutating, provider.fields, settings]);

  const save = () => {
    // Only send secret values the user actually typed: omitted keys keep the
    // stored secret (per-key merge on the server).
    const suppliedCredentials: FieldValues = {};
    for (const [key, value] of Object.entries(credentials)) {
      if (value.trim() !== "") suppliedCredentials[key] = value.trim();
    }
    upsert({
      request: {
        upsertConfigRequestBody2: {
          provider: provider.id,
          // A first-time connect starts enabled; the connection switch and
          // sheet manage the flag from then on.
          enabled: isConfigured ? enabled : true,
          credentials: suppliedCredentials,
          settings,
        },
      },
    });
  };

  // Instantly persists an enabled/disabled flip for an already-saved
  // connection without touching credentials or settings.
  const saveEnabled = (nextEnabled: boolean) => {
    setEnabled(nextEnabled);
    if (!isConfigured) return;
    upsert({
      request: {
        upsertConfigRequestBody2: {
          provider: provider.id,
          enabled: nextEnabled,
        },
      },
    });
  };

  const remove = () => {
    if (!isConfigured) return;
    deleteConfig({
      request: {
        deleteConfigRequestBody: { provider: provider.id },
      },
    });
  };

  const testConnection = () => {
    setTestResult(null);
    runTest({
      request: {
        // Speakeasy dedupes the identical { provider } body shape, so the
        // generated property name borrows the delete body's type.
        deleteConfigRequestBody: { provider: provider.id },
      },
    });
  };

  return {
    isLoading,
    enabled,
    isConfigured,
    hasSavedCredentials,
    credentials,
    setCredential: (key, value) =>
      setCredentials((current) => ({ ...current, [key]: value })),
    settings,
    setSetting: (key, value) =>
      setSettings((current) => ({ ...current, [key]: value })),
    isMutating,
    canSave,
    save,
    saveEnabled,
    remove,
    testConnection,
    isTesting: testStatus === "pending",
    testResult,
    clearTestResult: () => setTestResult(null),
  };
}
