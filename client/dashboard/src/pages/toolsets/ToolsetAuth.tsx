import { Input } from "@/components/ui/input";
import { useTelemetry } from "@/contexts/Telemetry";
import { useSdkClient } from "@/contexts/Sdk";
import { isHttpTool, Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { EnvironmentEntryInput } from "@gram/client/models/components";
import {
  invalidateAllListEnvironments,
  useGetToolsetEnvironment,
} from "@gram/client/react-query";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { Button } from "@speakeasy-api/moonshine";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Plus, TriangleAlert } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useEnvironments } from "../environments/Environments";
import { EnvironmentSelector } from "./EnvironmentSelector";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";

const PASSWORD_MASK = "••••••••";
const SECRET_FIELD_INDICATORS = ["SECRET", "KEY"] as const;

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

interface ToolsetAuthProps {
  toolset: Toolset;
  environmentSlug?: string;
  onEnvironmentChange?: (environmentSlug: string) => void;
}

export function ToolsetAuth({
  toolset,
  environmentSlug,
  onEnvironmentChange,
}: ToolsetAuthProps) {
  const environments = useEnvironments();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const sdkClient = useSdkClient();

  const [envValues, setEnvValues] = useState<Record<string, string>>({});
  const [environmentDirty, setEnvironmentDirty] = useState(false);
  const [editedFields, setEditedFields] = useState<Set<string>>(new Set());
  const [focusedField, setFocusedField] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Track attached environment state
  const [selectedEnvironmentSlug, setSelectedEnvironmentSlug] = useState<
    string | undefined
  >(undefined);
  const [initialAttachedEnvironmentId, setInitialAttachedEnvironmentId] =
    useState<string | undefined>(undefined);
  const [attachedEnvironmentChanged, setAttachedEnvironmentChanged] =
    useState(false);

  // Load the attached environment for this toolset
  const { data: attachedEnvironment, isLoading: isLoadingAttachedEnv, refetch: refetchAttachedEnvironment } =
    useGetToolsetEnvironment(
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

  const envSlug = selectedEnvironmentSlug || toolset.defaultEnvironmentSlug;
  const environment = environments.find((env) => env.slug === envSlug);

  // Initialize selected environment from attached environment
  useEffect(() => {
    if (attachedEnvironment) {
      setSelectedEnvironmentSlug(attachedEnvironment.slug);
      setInitialAttachedEnvironmentId(attachedEnvironment.id);
    }
  }, [attachedEnvironment]);

  // Single mutation that handles both environment updates and toolset link changes
  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!environment) {
        throw new Error("No environment selected");
      }

      const persistEnvironment = async () => {
        if (!environmentDirty) return;

        const { slug: environmentSlug } = environment;
        const isEmpty = (value: string) => !value || value.trim() === "";

        const entriesToUpdate: EnvironmentEntryInput[] = Object.entries(
          envValues,
        )
          .filter(([, value]) => !isEmpty(value))
          .map(([name, value]) => ({ name, value }));

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

      return await Promise.all([persistEnvironment(), persistAttachedEnvironment()]);
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
      setEnvironmentDirty(false);
      setAttachedEnvironmentChanged(false);
      setEnvValues({});
      setEditedFields(new Set());
      setInitialAttachedEnvironmentId(environment?.id);
      setSaveError(null);

      toast.success("Changes saved successfully");
    },
    onError: (error) => {
      console.error("Save failed:", error);
      setSaveError("Failed to save changes. Please try again.");
      toast.error("Failed to save changes. Please try again.");
    },
    onSettled: () => {
      refetchAttachedEnvironment();
    },
  });

  const { mutate: handleSave, isPending: isSaving } = saveMutation;

  const handleValueChange = useCallback(
    (varName: string, value: string) => {
      setEnvValues((prev) => ({ ...prev, [varName]: value }));
      setEditedFields((prev) => new Set(prev).add(varName));
      setEnvironmentDirty(true);
      if (saveError) setSaveError(null);
    },
    [saveError],
  );

  const handleEnvironmentSelectorChange = useCallback(
    (slug: string) => {
      setSelectedEnvironmentSlug(slug);
      const selectedEnv = environments.find((env) => env.slug === slug);
      if (selectedEnv?.id !== initialAttachedEnvironmentId) {
        setAttachedEnvironmentChanged(true);
      } else {
        setAttachedEnvironmentChanged(false);
      }
      if (onEnvironmentChange) {
        onEnvironmentChange(slug);
      }
      if (saveError) setSaveError(null);
    },
    [
      environments,
      initialAttachedEnvironmentId,
      onEnvironmentChange,
      saveError,
    ],
  );

  const handleFieldFocus = useCallback((varName: string) => {
    setFocusedField(varName);
  }, []);

  const handleFieldBlur = useCallback(() => {
    setFocusedField(null);
  }, []);

  const handleCancel = useCallback(() => {
    setEnvValues({});
    setEditedFields(new Set());
    setEnvironmentDirty(false);
    setAttachedEnvironmentChanged(false);
    setSelectedEnvironmentSlug(attachedEnvironment?.slug);
    setSaveError(null);
  }, [attachedEnvironment?.slug]);

  const requiresServerURL =
    toolset.tools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  const relevantEnvVars = useToolsetEnvVars(toolset, requiresServerURL);

  const environmentVariableInputs = useMemo(() => {
    return relevantEnvVars.map((varName) => {
      const entry =
        environment?.entries?.find((e) => e.name === varName) ?? null;
      const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
        varName.includes(indicator),
      );
      const inputValue = envValues[varName] ?? "";
      const hasExistingValue =
        entry?.value != null && entry.value.trim() !== "";
      const isEdited = editedFields.has(varName);
      const isFocused = focusedField === varName;

      let displayValue = "";
      if (isEdited) {
        displayValue = inputValue;
      } else if (!isFocused && hasExistingValue && entry?.value) {
        displayValue = isSecret ? PASSWORD_MASK : entry.value;
      }

      return {
        varName,
        entry,
        isSecret,
        inputValue,
        hasExistingValue,
        isEdited,
        isFocused,
        displayValue,
      };
    });
  }, [
    relevantEnvVars,
    environment?.entries,
    envValues,
    editedFields,
    focusedField,
  ]);

  useEffect(() => {
    setEnvValues({});
    setEditedFields(new Set());
    setEnvironmentDirty(false);
    setSaveError(null);
  }, [environment?.slug]);

  const hasChanges = environmentDirty || attachedEnvironmentChanged;

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" && hasChanges) {
        handleSave();
      } else if (e.key === "Escape" && hasChanges) {
        handleCancel();
        e.currentTarget.blur();
      }
    },
    [hasChanges, handleSave, handleCancel],
  );

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
            selectedEnvironment={selectedEnvironmentSlug ?? ""}
            setSelectedEnvironment={handleEnvironmentSelectorChange}
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

      {!selectedEnvironmentSlug && (
        <div className="flex flex-col items-center justify-center py-12 px-4 border border-dashed rounded-lg">
          <p className="text-sm text-muted-foreground mb-4">
            No currently attached environment. Choose one:
          </p>
          <div className="flex items-center gap-2">
            <EnvironmentSelector
              selectedEnvironment={selectedEnvironmentSlug ?? ""}
              setSelectedEnvironment={handleEnvironmentSelectorChange}
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

      {selectedEnvironmentSlug && (
        <>
          {relevantEnvVars.length > 0 && (
        <div className="space-y-6">
          <div className="space-y-4">
            {environmentVariableInputs.map(
              ({ varName, isSecret, displayValue, hasExistingValue }) => (
                <div
                  key={varName}
                  className="grid grid-cols-2 gap-4 items-start"
                >
                  <label
                    htmlFor={`env-${varName}`}
                    className="text-sm font-medium text-foreground break-words pt-2"
                  >
                    {varName}
                  </label>
                  <Input
                    id={`env-${varName}`}
                    value={displayValue}
                    onChange={(value) => handleValueChange(varName, value)}
                    onFocus={() => handleFieldFocus(varName)}
                    onBlur={handleFieldBlur}
                    onKeyDown={handleKeyDown}
                    placeholder={
                      hasExistingValue
                        ? "Replace existing value"
                        : "Enter value"
                    }
                    type={isSecret ? "password" : "text"}
                    className="font-mono text-sm"
                    disabled={isSaving}
                    autoComplete={isSecret ? "new-password" : "off"}
                  />
                </div>
              ),
            )}
          </div>

          {hasChanges && (
            <SaveActionBar
              saveError={saveError}
              isSaving={isSaving}
              onSave={handleSave}
              onCancel={handleCancel}
            />
          )}
        </div>
          )}

          {relevantEnvVars.length === 0 && (
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
