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
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useEnvironments } from "@/pages/environments/Environments";
import { EnvironmentSelector } from "@/pages/toolsets/EnvironmentSelector";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import {
  EnvironmentEntryInput,
  EnvironmentEntryInputProps,
} from "./EnvironmentEntryInput";

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY"] as const;

// Hook for the submit mutation
function useEnvironmentFormSubmitMutation({
  environment,
  environmentEntries,
  environmentDirty,
  attachedEnvironmentChanged,
  toolset,
  invalidate,
}: {
  environment: Environment | null;
  environmentEntries: EnvironmentEntryFormInput[];
  environmentDirty: boolean;
  attachedEnvironmentChanged: boolean;
  toolset: Toolset;
  invalidate: () => void;
}) {
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async () => {
      if (!environment) {
        throw new Error("No environment selected");
      }

      const persistEnvironment = async () => {
        if (!environmentDirty) return;

        const { slug: environmentSlug } = environment;

        const entriesToUpdate: EnvironmentEntryInputType[] = environmentEntries
          .filter((entry) => entry.inputValue.trim() !== "")
          .map((entry) => ({ name: entry.varName, value: entry.inputValue }));

        await sdkClient.environments.updateBySlug({
          slug: environmentSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate,
            entriesToRemove: [],
          },
        });
      };

      const persistAttachedEnvironment = async () => {
        if (!attachedEnvironmentChanged) return;

        if (environment.id) {
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
      };

      return await Promise.all([
        persistEnvironment(),
        persistAttachedEnvironment(),
      ]);
    },
    onSuccess: () => {
      if (environmentDirty) {
        telemetry.capture("environment_event", {
          action: "environment_updated_from_toolset_auth",
        });
      }
      if (attachedEnvironmentChanged) {
        telemetry.capture("toolset_event", {
          action: environment?.id
            ? "toolset_environment_attached"
            : "toolset_environment_detached",
        });
      }
      invalidateAllListEnvironments(queryClient);

      toast.success("Changes saved successfully");
    },
    onError: () => {
      toast.error("Failed to save changes. Please try again.");
    },
    onSettled: () => {
      invalidate();
    },
  });
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
  const environments = useEnvironments();

  const [environmentEntries, setEnvironmentEntries] = useState<
    EnvironmentEntryFormInput[]
  >([]);
  const [environment, setEnvironment] = useState<Environment | null>(null);

  useRegisterEnvironmentTelemetry({
    environmentSlug: environment?.slug ?? "",
  });

  // Load the attached environment for this toolset
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

  // Derive environmentDirty from environmentEntries
  const environmentDirty = useMemo(() => {
    return environmentEntries.some((entry) => entry.inputValue !== "");
  }, [environmentEntries]);

  // Derive attachedEnvironmentChanged from environment vs query data
  const attachedEnvironmentChanged = useMemo(() => {
    return environment?.id !== attachedEnvironmentQuery.data?.id;
  }, [environment?.id, attachedEnvironmentQuery.data?.id]);

  const isDirty = environmentDirty || attachedEnvironmentChanged;

  // Sync environment from query result when not dirty
  useEffect(() => {
    if (!isDirty) {
      setEnvironment(attachedEnvironmentQuery.data ?? null);
    }
  }, [attachedEnvironmentQuery.data, isDirty]);

  // Debug logging
  useEffect(() => {
    console.log("[ToolsetEnvironmentForm] environment changed:", environment);
  }, [environment]);

  useEffect(() => {
    console.log(
      "[ToolsetEnvironmentForm] attachedEnvironmentQuery.data changed:",
      attachedEnvironmentQuery.data,
    );
  }, [attachedEnvironmentQuery.data]);

  useEffect(() => {
    console.log("[ToolsetEnvironmentForm] dirty state:", {
      isDirty,
      environmentDirty,
      attachedEnvironmentChanged,
    });
  }, [isDirty, environmentDirty, attachedEnvironmentChanged]);

  // Single mutation that handles both environment updates and toolset link changes
  const saveMutation = useEnvironmentFormSubmitMutation({
    environment,
    environmentEntries,
    environmentDirty,
    attachedEnvironmentChanged,
    toolset,
    invalidate: attachedEnvironmentQuery.refetch,
  });

  const { mutate: handleSave, isPending: isSaving } = saveMutation;

  const isLoading = attachedEnvironmentQuery.isLoading || isSaving;

  const handleValueChange = useCallback((varName: string, value: string) => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) =>
        entry.varName === varName ? { ...entry, inputValue: value } : entry,
      ),
    );
  }, []);

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
    // Reset all inputValues to empty
    setEnvironmentEntries((prev) =>
      prev.map((entry) => ({ ...entry, inputValue: "" })),
    );
    // Reset environment to server data
    setEnvironment(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data]);

  const requiresServerURL =
    toolset.tools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  const relevantEnvVars = useToolsetEnvVars(toolset, requiresServerURL);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      const isDirty = environmentDirty || attachedEnvironmentChanged;
      if (e.key === "Enter" && isDirty) {
        handleSave();
      } else if (e.key === "Escape" && isDirty) {
        handleCancel();
        e.currentTarget.blur();
      }
    },
    [environmentDirty, attachedEnvironmentChanged, handleSave, handleCancel],
  );

  // Helper function to generate input props from an entry
  const getInputPropsForEntry = useCallback(
    (entry: EnvironmentEntryFormInput): EnvironmentEntryInputProps => {
      return {
        varName: entry.varName,
        isSensitive: entry.isSensitive,
        inputValue: entry.inputValue,
        entryValue: entry.kind === "persisted" ? entry.initialValue : null,
        hasExistingValue: entry.kind === "persisted",
        isDirty: entry.inputValue !== "",
        isSaving: isLoading,
        onValueChange: handleValueChange,
        onKeyDown: handleKeyDown,
      };
    },
    [isLoading, handleValueChange, handleKeyDown],
  );

  const environmentVariableInputs = useMemo<EnvironmentEntryFormInput[]>(() => {
    return environmentEntries;
  }, [environmentEntries]);

  // Initialize environmentEntries when environment or relevantEnvVars changes
  useEffect(() => {
    console.log("[ToolsetEnvironmentForm] Initializing environmentEntries for:", {
      environmentSlug: environment?.slug,
      environmentId: environment?.id,
      entriesCount: environment?.entries?.length,
      relevantEnvVars,
    });

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

    console.log("[ToolsetEnvironmentForm] Setting environmentEntries to:", initialValues);
    setEnvironmentEntries(initialValues);
  }, [environment?.slug, environment?.entries, relevantEnvVars]);

  return {
    selectedEnvironment: environment,
    onEnvironmentSelectorChange: handleEnvironmentSelectorChange,
    environmentVariableInputs,
    getInputPropsForEntry,
    isDirty,
    saveError: saveMutation.error
      ? "Failed to save changes. Please try again."
      : null,
    isSaving: isLoading,
    onSubmit: handleSave,
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
            <div className="space-y-6">
              <div className="space-y-4">
                {form.environmentVariableInputs.map((input) => (
                  <EnvironmentEntryInput
                    key={input.varName}
                    {...form.getInputPropsForEntry(input)}
                  />
                ))}
              </div>

              {form.isDirty && (
                <SaveActionBar
                  saveError={form.saveError}
                  isSaving={form.isSaving}
                  onSave={form.onSubmit}
                  onCancel={form.onCancel}
                />
              )}
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
    </div>
  );
}
