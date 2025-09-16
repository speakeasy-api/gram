import { useState, useEffect, useMemo, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Plus } from "lucide-react";

import { Button } from "@speakeasy-api/moonshine";
import { Input } from "@/components/ui/input";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";

import { Toolset, EnvironmentEntryInput } from "@gram/client/models/components";
import {
  invalidateAllListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";

import { useEnvironments } from "../environments/Environments";
import { EnvironmentSelector } from "./EnvironmentSelector";

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
        <Button type="button"
          variant="tertiary"
          size="sm"
          onClick={onCancel}
          disabled={isSaving}
          aria-label="Cancel changes"
        >
          Cancel
        </Button>
        <Button type="button"
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

  const [envValues, setEnvValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);
  const [editedFields, setEditedFields] = useState<Set<string>>(new Set());
  const [focusedField, setFocusedField] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const envSlug = environmentSlug || toolset.defaultEnvironmentSlug;
  const environment = environments.find((env) => env.slug === envSlug);

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_updated_from_toolset_auth",
      });
      invalidateAllListEnvironments(queryClient);
      setHasChanges(false);
      setSaveError(null);
      setEnvValues({});
      setEditedFields(new Set());
    },
    onError: (error) => {
      console.error(
        "Environment variable save failed:",
        error?.message || error
      );
      setSaveError("Failed to save environment variables. Please try again.");
    },
  });

  const { mutate: updateEnvironment, isPending: isSaving } =
    updateEnvironmentMutation;

  const handleValueChange = useCallback(
    (varName: string, value: string) => {
      setEnvValues((prev) => ({ ...prev, [varName]: value }));
      setEditedFields((prev) => new Set(prev).add(varName));
      setHasChanges(true);
      if (saveError) setSaveError(null);
    },
    [saveError]
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
    setHasChanges(false);
    setSaveError(null);
  }, []);

  const requiresServerURL = toolset.httpTools?.some(
    (tool) => !tool.defaultServerUrl
  );

  const relevantEnvVars = useMemo(() => {
    const securityVars =
      toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      [];
    const serverVars =
      toolset?.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter(
          (v) => !v.toLowerCase().includes("server_url") || requiresServerURL
        )
      ) ?? [];

    return [...securityVars, ...serverVars];
  }, [toolset?.securityVariables, toolset?.serverVariables, requiresServerURL]);

  const environmentVariableInputs = useMemo(() => {
    return relevantEnvVars.map((varName) => {
      const entry =
        environment?.entries?.find((e) => e.name === varName) ?? null;
      const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
        varName.includes(indicator)
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
    setHasChanges(false);
    setSaveError(null);
  }, [environment?.slug]);

  const handleSave = useCallback(() => {
    if (!environment) return;

    const { entries: environmentEntries, slug: environmentSlug } = environment;
    const isEmpty = (value: string) => !value || value.trim() === "";

    const entriesToUpdate: EnvironmentEntryInput[] = Object.entries(envValues)
      .filter(([, value]) => !isEmpty(value))
      .map(([name, value]) => ({ name, value }));

    const entriesToRemove: string[] = [];

    if (environmentEntries) {
      relevantEnvVars.forEach((varName) => {
        const currentValue = environmentEntries.find(
          (e) => e.name === varName
        )?.value;
        const newValue = envValues[varName] ?? "";

        if (currentValue && isEmpty(newValue)) {
          entriesToRemove.push(varName);
        }
      });
    }

    updateEnvironment({
      request: {
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate,
          entriesToRemove,
        },
      },
    });
  }, [environment, envValues, updateEnvironment, relevantEnvVars]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" && hasChanges) {
        handleSave();
      } else if (e.key === "Escape" && hasChanges) {
        setEnvValues({});
        setEditedFields(new Set());
        setHasChanges(false);
        setSaveError(null);
        e.currentTarget.blur();
      }
    },
    [hasChanges, handleSave]
  );

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between">
        <div className="space-y-1">
          <h2 className="text-heading-xs">Environment Variables</h2>
          <p className="text-sm text-muted-foreground">
            Configure required API credentials for this toolset to use in the Gram dashboard
          </p>
          <p className="text-sm text-muted-foreground">
            View the MCP page for options on how to provide relevant credentials to an MCP server
          </p>
        </div>
        <div className="flex-shrink-0 flex items-center gap-2">
          <EnvironmentSelector
            selectedEnvironment={environment?.slug ?? ""}
            setSelectedEnvironment={(slug) => {
              if (onEnvironmentChange) {
                onEnvironmentChange(slug);
              }
            }}
            className="h-8"
          />
          <Button type="button"
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

      {relevantEnvVars.length > 0 && (
        <div className="space-y-6">
          <div className="space-y-4">
            {environmentVariableInputs.map(
              ({ varName, isSecret, displayValue, hasExistingValue }) => (
                <div
                  key={varName}
                  className="grid grid-cols-2 gap-4 items-center"
                >
                  <label
                    htmlFor={`env-${varName}`}
                    className="text-sm font-medium text-foreground"
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
              )
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
    </div>
  );
}
