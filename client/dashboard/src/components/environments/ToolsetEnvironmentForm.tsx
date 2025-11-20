import {
  useRegisterEnvironmentTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useSdkClient } from "@/contexts/Sdk";
import { isHttpTool, Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import {
  Environment,
  EnvironmentEntryInput as EnvironmentEntryInputType,
} from "@gram/client/models/components";
import {
  invalidateAllListEnvironments,
  useGetToolsetEnvironment,
} from "@gram/client/react-query";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { Button } from "@speakeasy-api/moonshine";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Plus, TriangleAlert, X } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useEnvironments } from "@/pages/environments/Environments";
import { EnvironmentSelector } from "@/pages/toolsets/EnvironmentSelector";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import {
  EnvironmentEntryInput,
  EnvironmentEntryInputProps,
} from "./EnvironmentEntryInput";

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY"] as const;

// Hook for managing which environment is attached to a toolset
interface UseAttachedEnvironmentFormParams {
  toolset: Toolset;
  onEnvironmentChange?: (slug: string) => void;
}

interface UseAttachedEnvironmentFormReturn {
  selectedEnvironment: Environment | null;
  onEnvironmentSelectorChange: (slug: string) => void;
  isDirty: boolean;
  persist: () => Promise<void>;
  cancel: () => void;
  isLoading: boolean;
  error: string | null;
}

function useAttachedEnvironmentForm({
  toolset,
  onEnvironmentChange,
}: UseAttachedEnvironmentFormParams): UseAttachedEnvironmentFormReturn {
  const environments = useEnvironments();
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();

  const [environment, setEnvironment] = useState<Environment | null>(null);
  const [isDirty, setIsDirty] = useState(false);

  const attachedEnvironmentQuery = useGetToolsetEnvironment(
    {
      toolsetId: toolset.id,
    },
    undefined,
    {
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );

  useEffect(() => {
    setIsDirty(environment?.id !== attachedEnvironmentQuery.data?.id);
  }, [environment?.id, attachedEnvironmentQuery.data?.id]);

  useEffect(() => {
    if (!isDirty) {
      setEnvironment(attachedEnvironmentQuery.data ?? null);
    }
  }, [attachedEnvironmentQuery.data, isDirty]);

  const persist = useCallback(async () => {
    if (!isDirty) return;

    try {
      if (environment?.id) {
        await sdkClient.environments.setToolsetLink({
          setToolsetEnvironmentLinkRequestBody: {
            toolsetId: toolset.id,
            environmentId: environment.id,
          },
        });
      } else {
        await sdkClient.environments.deleteToolsetLink({
          toolsetId: toolset.id,
        });
      }

      telemetry.capture("toolset_event", {
        action: environment?.id
          ? "toolset_environment_attached"
          : "toolset_environment_detached",
      });

      setIsDirty(false);
    } finally {
      await attachedEnvironmentQuery.refetch();
    }
  }, [
    isDirty,
    environment,
    sdkClient,
    toolset.id,
    telemetry,
    attachedEnvironmentQuery,
  ]);

  const handleEnvironmentSelectorChange = useCallback(
    (slug: string) => {
      const selectedEnv = environments.find((env) => env.slug === slug);
      setEnvironment(selectedEnv ?? null);
      if (onEnvironmentChange) {
        onEnvironmentChange(slug);
      }
    },
    [environments, onEnvironmentChange],
  );

  const handleCancel = useCallback(() => {
    setEnvironment(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data]);

  return {
    selectedEnvironment: environment,
    onEnvironmentSelectorChange: handleEnvironmentSelectorChange,
    isDirty,
    persist,
    cancel: handleCancel,
    isLoading: attachedEnvironmentQuery.isLoading,
    error: null, // Errors handled by parent mutation
  };
}

// Hook for managing environment variable entries
interface UseEnvironmentEntriesFormParams {
  environment: Environment | null;
  relevantEnvVars: string[];
}

interface UseEnvironmentEntriesFormReturn {
  entries: EnvironmentEntryFormInput[];
  getInputPropsForEntry: (
    entry: EnvironmentEntryFormInput,
  ) => EnvironmentEntryInputProps;
  isDirty: boolean;
  persist: () => Promise<void>;
  cancel: () => void;
  isLoading: boolean;
  error: string | null;
}

function useEnvironmentEntriesForm({
  environment,
  relevantEnvVars,
}: UseEnvironmentEntriesFormParams): UseEnvironmentEntriesFormReturn {
  const queryClient = useQueryClient();
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();

  const [environmentEntries, setEnvironmentEntries] = useState<
    EnvironmentEntryFormInput[]
  >([]);
  const [isDirty, setIsDirty] = useState(false);

  useEffect(() => {
    const initialValues: EnvironmentEntryFormInput[] = relevantEnvVars.map(
      (varName) => {
        const entry = environment?.entries?.find((e) => e.name === varName);
        const isSensitive = SECRET_FIELD_INDICATORS.some((indicator) =>
          varName.includes(indicator),
        );

        if (entry?.value != null && entry.value.trim() !== "") {
          return {
            kind: "persisted" as const,
            varName,
            isSensitive,
            initialValue: entry.value,
            inputValue: "",
          };
        } else {
          return {
            kind: "new" as const,
            varName,
            isSensitive,
            inputValue: "",
          };
        }
      },
    );

    setEnvironmentEntries(initialValues);
  }, [environment?.slug, environment?.entries, relevantEnvVars]);

  useEffect(() => {
    setIsDirty(environmentEntries.some((entry) => entry.inputValue !== ""));
  }, [environmentEntries]);

  // Persist function
  const persist = useCallback(async () => {
    if (!isDirty || !environment) return;

    const { slug: environmentSlug } = environment;

    const entriesToUpdate: EnvironmentEntryInputType[] = environmentEntries
      .filter((entry) => entry.inputValue.trim() !== "")
      .map((entry) => ({ name: entry.varName, value: entry.inputValue }));

    try {
      await sdkClient.environments.updateBySlug({
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate,
          entriesToRemove: [],
        },
      });

      telemetry.capture("environment_event", {
        action: "environment_updated_from_toolset_auth",
      });

      setIsDirty(false);
    } finally {
      invalidateAllListEnvironments(queryClient);
    }
  }, [
    isDirty,
    environment,
    environmentEntries,
    sdkClient,
    telemetry,
    queryClient,
  ]);

  const handleValueChange = useCallback((varName: string, value: string) => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) =>
        entry.varName === varName ? { ...entry, inputValue: value } : entry,
      ),
    );
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Escape" && isDirty) {
        setEnvironmentEntries((prev) =>
          prev.map((entry) => ({ ...entry, inputValue: "" })),
        );
        e.currentTarget.blur();
      }
    },
    [isDirty],
  );

  const getInputPropsForEntry = useCallback(
    (entry: EnvironmentEntryFormInput): EnvironmentEntryInputProps => {
      return {
        varName: entry.varName,
        isSensitive: entry.isSensitive,
        inputValue: entry.inputValue,
        entryValue: entry.kind === "persisted" ? entry.initialValue : null,
        hasExistingValue: entry.kind === "persisted",
        isDirty: entry.inputValue !== "",
        isSaving: false, // Will be controlled by parent
        onValueChange: handleValueChange,
        onKeyDown: handleKeyDown,
      };
    },
    [handleValueChange, handleKeyDown],
  );

  const handleCancel = useCallback(() => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) => ({ ...entry, inputValue: "" })),
    );
  }, []);

  return {
    entries: environmentEntries,
    getInputPropsForEntry,
    isDirty,
    persist,
    cancel: handleCancel,
    isLoading: false, // No loading state needed
    error: null, // Errors handled by parent mutation
  };
}

// Types for the hook
interface PersistedEnvironmentEntry {
  kind: "persisted";
  varName: string;
  isSensitive: boolean;
  initialValue: string;
  inputValue: string;
}

interface NewEnvironmentEntry {
  kind: "new";
  varName: string;
  isSensitive: boolean;
  inputValue: string;
}

type EnvironmentEntryFormInput =
  | PersistedEnvironmentEntry
  | NewEnvironmentEntry;

interface UseToolsetEnvironmentFormParams {
  toolset: Toolset;
  onEnvironmentChange?: (slug: string) => void;
}

interface UseToolsetEnvironmentFormReturn {
  selectedEnvironment: Environment | null;
  onEnvironmentSelectorChange: (slug: string) => void;
  environmentVariableInputs: EnvironmentEntryFormInput[];
  getInputPropsForEntry: (
    entry: EnvironmentEntryFormInput,
  ) => EnvironmentEntryInputProps;
  isDirty: boolean;
  saveError: string | null;
  isSaving: boolean;
  onSubmit: () => void;
  onCancel: () => void;
  relevantEnvVars: string[];
}

function useToolsetEnvironmentForm({
  toolset,
  onEnvironmentChange,
}: UseToolsetEnvironmentFormParams): UseToolsetEnvironmentFormReturn {
  const requiresServerURL =
    toolset.tools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  const relevantEnvVars = useToolsetEnvVars(toolset, requiresServerURL);

  // Use the attached environment form hook
  const attachedEnvForm = useAttachedEnvironmentForm({
    toolset,
    onEnvironmentChange,
  });

  // Use the environment entries form hook
  const entriesForm = useEnvironmentEntriesForm({
    environment: attachedEnvForm.selectedEnvironment,
    relevantEnvVars,
  });

  useRegisterEnvironmentTelemetry({
    environmentSlug: attachedEnvForm.selectedEnvironment?.slug ?? "",
  });

  // Combined dirty state
  const isDirty = attachedEnvForm.isDirty || entriesForm.isDirty;

  // Combined mutation
  const mutation = useMutation({
    mutationFn: async () => {
      await Promise.all([attachedEnvForm.persist(), entriesForm.persist()]);
    },
  });

  // Combined save error
  const saveError =
    attachedEnvForm.error || entriesForm.error || mutation.error
      ? "Failed to save changes"
      : null;

  // Combined loading state
  const isSaving =
    attachedEnvForm.isLoading || entriesForm.isLoading || mutation.isPending;

  // Combined cancel handler
  const handleCancel = useCallback(() => {
    attachedEnvForm.cancel();
    entriesForm.cancel();
  }, [attachedEnvForm, entriesForm]);

  return {
    selectedEnvironment: attachedEnvForm.selectedEnvironment,
    onEnvironmentSelectorChange: attachedEnvForm.onEnvironmentSelectorChange,
    environmentVariableInputs: entriesForm.entries,
    getInputPropsForEntry: entriesForm.getInputPropsForEntry,
    isDirty,
    saveError,
    isSaving,
    onSubmit: mutation.mutate,
    onCancel: handleCancel,
    relevantEnvVars,
  };
}

interface SaveActionBarProps {
  saveError: string | null;
  isSaving: boolean;
  onSave: () => void;
  onCancel: () => void;
}

function SaveActionBar({
  saveError,
  isSaving,
  onSave,
  onCancel,
}: SaveActionBarProps) {
  return (
    <div className="flex items-center justify-between pt-4 border-t">
      {saveError && (
        <div
          className="flex items-center gap-2 text-sm text-destructive"
          role="alert"
        >
          <AlertCircle className="h-4 w-4" aria-hidden="true" />
          {saveError}
        </div>
      )}
      <div className="flex items-center gap-3 ml-auto">
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={onCancel}
          disabled={isSaving}
          aria-label="Cancel changes"
        >
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={onSave}
          disabled={isSaving}
          aria-label={
            isSaving
              ? "Saving environment variables"
              : "Save environment variables"
          }
        >
          {isSaving ? "Saving..." : "Save"}
        </Button>
      </div>
    </div>
  );
}

interface ToolsetEnvironmentFormProps {
  toolset: Toolset;
}

export function ToolsetEnvironmentForm({
  toolset,
}: ToolsetEnvironmentFormProps) {
  const routes = useRoutes();

  const form = useToolsetEnvironmentForm({
    toolset,
  });

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between">
        <div className="space-y-1">
          <h2 className="text-heading-xs">Attached Environment</h2>
          <p className="text-sm text-muted-foreground">
            Configure required API credentials for this toolset to use in the
            Gram dashboard
          </p>
          <p className="text-sm text-muted-foreground">
            View the MCP page for options on how to provide relevant credentials
            to an MCP server
          </p>
        </div>
        <div className="flex-shrink-0 flex items-center gap-2">
          <EnvironmentSelector
            selectedEnvironment={form.selectedEnvironment?.slug ?? ""}
            setSelectedEnvironment={form.onEnvironmentSelectorChange}
            className="h-8"
          />
          {form.selectedEnvironment && (
            <Button
              type="button"
              variant="tertiary"
              size="sm"
              onClick={() => form.onEnvironmentSelectorChange("")}
              aria-label="Clear environment"
            >
              <X className="h-4 w-4 mr-1" aria-hidden="true" />
              Clear
            </Button>
          )}
        </div>
      </div>

      <div className="flex items-start gap-2 px-4 py-3 bg-warning/10 border border-warning/20 rounded-md">
        <TriangleAlert
          className="h-4 w-4 text-warning flex-shrink-0 mt-0.5"
          aria-hidden="true"
        />
        <p className="text-sm text-warning">
          Environments attached here will apply to all users of tools from this
          toolset in both public and private servers
        </p>
      </div>

      {!form.selectedEnvironment && (
        <div className="flex flex-col items-center justify-center py-12 px-4 border border-dashed rounded-lg">
          <p className="text-sm text-muted-foreground mb-4">
            No currently attached environment. Choose one:
          </p>
          <div className="flex items-center gap-2">
            <EnvironmentSelector
              selectedEnvironment=""
              setSelectedEnvironment={form.onEnvironmentSelectorChange}
              className="h-8"
            />
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => routes.environments.goTo()}
              aria-label="Add new environment"
            >
              <Plus className="h-4 w-4 mr-1" aria-hidden="true" />
              Add New
            </Button>
          </div>
        </div>
      )}

      {form.selectedEnvironment && (
        <>
          {form.relevantEnvVars.length > 0 && (
            <div className="space-y-4">
              {form.environmentVariableInputs.map((input) => (
                <EnvironmentEntryInput
                  key={input.varName}
                  {...form.getInputPropsForEntry(input)}
                />
              ))}
            </div>
          )}

          {form.relevantEnvVars.length === 0 && (
            <div className="text-center py-8">
              <p className="text-sm text-muted-foreground">
                No authentication required for this toolset
              </p>
            </div>
          )}
        </>
      )}

      {form.isDirty && (
        <SaveActionBar
          saveError={form.saveError}
          isSaving={form.isSaving}
          onSave={form.onSubmit}
          onCancel={form.onCancel}
        />
      )}
    </div>
  );
}
