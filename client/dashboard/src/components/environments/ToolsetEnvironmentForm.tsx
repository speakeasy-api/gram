import { useRegisterEnvironmentTelemetry } from "@/contexts/Telemetry";
import { isHttpTool, Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { Environment } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { useMutation } from "@tanstack/react-query";
import { AlertCircle, Plus, TriangleAlert, X } from "lucide-react";
import { useCallback } from "react";
import { EnvironmentSelector } from "@/pages/toolsets/EnvironmentSelector";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import { useAttachedEnvironmentForm } from "./useAttachedEnvironmentForm";
import {
  EnvironmentEntriesFormFields,
  useEnvironmentEntriesForm,
  EnvironmentEntryFormInput,
} from "./EnvironmentEntriesFormFields";

interface UseToolsetEnvironmentFormParams {
  toolset: Toolset;
  onEnvironmentChange?: (slug: string) => void;
}

interface UseToolsetEnvironmentFormReturn {
  selectedEnvironment: Environment | null;
  onEnvironmentSelectorChange: (slug: string) => void;
  isDirty: boolean;
  saveError: string | null;
  isSaving: boolean;
  onSubmit: () => void;
  onCancel: () => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
  relevantEnvVars: string[];
  entries: EnvironmentEntryFormInput[];
}

function useToolsetEnvironmentForm({
  toolset,
  onEnvironmentChange,
}: UseToolsetEnvironmentFormParams): UseToolsetEnvironmentFormReturn {
  const requiresServerURL =
    toolset.tools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  const relevantEnvVars = useToolsetEnvVars(toolset, requiresServerURL);

  const attachedEnvForm = useAttachedEnvironmentForm({
    toolsetId: toolset.id,
    onEnvironmentChange,
  });

  const entriesForm = useEnvironmentEntriesForm({
    environment: attachedEnvForm.selectedEnvironment,
    relevantEnvVars,
  });

  useRegisterEnvironmentTelemetry({
    environmentSlug: attachedEnvForm.selectedEnvironment?.slug ?? "",
  });

  const isDirty = attachedEnvForm.isDirty || entriesForm.isDirty;

  const mutation = useMutation({
    mutationFn: async () => {
      await Promise.all([attachedEnvForm.persist(), entriesForm.persist()]);
    },
  });

  const saveError = mutation.error ? "Failed to save changes" : null;

  const isSaving = attachedEnvForm.isLoading || mutation.isPending;

  const handleCancel = useCallback(() => {
    attachedEnvForm.cancel();
    entriesForm.cancel();
  }, [attachedEnvForm, entriesForm]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Escape" && isDirty) {
        handleCancel();
        e.currentTarget.blur();
      } else if (e.key === "Enter" && isDirty) {
        mutation.mutate();
      }
    },
    [isDirty, handleCancel, mutation],
  );

  return {
    selectedEnvironment: attachedEnvForm.selectedEnvironment,
    onEnvironmentSelectorChange: attachedEnvForm.onEnvironmentSelectorChange,
    isDirty,
    saveError,
    isSaving,
    onSubmit: mutation.mutate,
    onCancel: handleCancel,
    onKeyDown: handleKeyDown,
    relevantEnvVars,
    entries: entriesForm.entries,
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
        <EnvironmentEntriesFormFields
          entries={form.entries}
          relevantEnvVars={form.relevantEnvVars}
          disabled={form.isSaving}
          onKeyDown={form.onKeyDown}
        />
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
