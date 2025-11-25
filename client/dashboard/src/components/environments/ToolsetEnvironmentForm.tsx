import { useRegisterEnvironmentTelemetry } from "@/contexts/Telemetry";
import { isHttpTool, Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { Environment } from "@gram/client/models/components";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { AlertCircle, ChevronDown, Plus, TriangleAlert, X } from "lucide-react";
import { useCallback, useState } from "react";
import { EnvironmentSelector } from "@/pages/toolsets/EnvironmentSelector";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import { useAttachedEnvironmentForm } from "./useAttachedEnvironmentForm";
import {
  EnvironmentEntriesFormFields,
  useEnvironmentForm,
  EnvironmentEntryFormInput,
} from "./EnvironmentEntriesFormFields";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

interface ActionBarProps {
  error?: string | null;
  isLoading: boolean;
  isDirty: boolean;
  onSave: () => void;
  onCancel: () => void;
  saveLabel: string;
  savingLabel: string;
}

function ActionBar({
  error,
  isLoading,
  isDirty,
  onSave,
  onCancel,
  saveLabel,
  savingLabel,
}: ActionBarProps) {
  return (
    <div className="flex items-center justify-between pt-4">
      {error && (
        <div
          className="flex items-center gap-2 text-sm text-destructive"
          role="alert"
        >
          <AlertCircle className="h-4 w-4" aria-hidden="true" />
          {error}
        </div>
      )}
      <div className="flex items-center gap-3 ml-auto">
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={onCancel}
          disabled={!isDirty || isLoading}
          aria-label="Cancel changes"
        >
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={onSave}
          disabled={!isDirty || isLoading}
          aria-label={isLoading ? savingLabel : saveLabel}
        >
          {isLoading ? savingLabel : saveLabel}
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
  const [isAdvancedOpen, setIsAdvancedOpen] = useState(false);

  const requiresServerURL =
    toolset.tools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  const relevantEnvVars = useToolsetEnvVars(toolset, requiresServerURL);

  const attachedEnvForm = useAttachedEnvironmentForm({
    toolsetId: toolset.id,
  });

  const entriesForm = useEnvironmentForm({
    environment: attachedEnvForm.selectedEnvironment,
    relevantEnvVars,
  });

  useRegisterEnvironmentTelemetry({
    environmentSlug: attachedEnvForm.selectedEnvironment?.slug ?? "",
  });

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Escape" && entriesForm.isDirty) {
        entriesForm.cancel();
        e.currentTarget.blur();
      } else if (e.key === "Enter" && entriesForm.isDirty) {
        entriesForm.mutation.mutate();
      }
    },
    [entriesForm],
  );

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between">
        <div className="space-y-1">
          <h2 className="text-heading-xs">Environment Variables</h2>
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
            selectedEnvironment={
              attachedEnvForm.selectedEnvironment?.slug ?? ""
            }
            setSelectedEnvironment={attachedEnvForm.onEnvironmentSelectorChange}
            className="h-8"
          />
          {attachedEnvForm.selectedEnvironment && (
            <Button
              type="button"
              variant="tertiary"
              size="sm"
              onClick={() => attachedEnvForm.onEnvironmentSelectorChange("")}
              aria-label="Clear environment"
            >
              <X className="h-4 w-4 mr-1" aria-hidden="true" />
              Clear
            </Button>
          )}
        </div>
      </div>

      {!attachedEnvForm.selectedEnvironment && (
        <div className="flex flex-col items-center justify-center py-12 px-4 border border-dashed rounded-lg">
          <p className="text-sm text-muted-foreground mb-4">
            No currently attached environment. Choose one:
          </p>
          <div className="flex items-center gap-2">
            <EnvironmentSelector
              selectedEnvironment=""
              setSelectedEnvironment={
                attachedEnvForm.onEnvironmentSelectorChange
              }
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

      {attachedEnvForm.selectedEnvironment && (
        <>
          <EnvironmentEntriesFormFields
            entries={entriesForm.entries}
            relevantEnvVars={relevantEnvVars}
            disabled={entriesForm.mutation.isPending}
            onKeyDown={handleKeyDown}
          />

          <ActionBar
            error={entriesForm.mutation.error ? "Failed to save changes" : null}
            isLoading={entriesForm.mutation.isPending}
            isDirty={entriesForm.isDirty}
            onSave={() => entriesForm.mutation.mutate()}
            onCancel={entriesForm.cancel}
            saveLabel="Save"
            savingLabel="Saving..."
          />
        </>
      )}

      <Collapsible
        open={isAdvancedOpen}
        onOpenChange={setIsAdvancedOpen}
        className="pt-4 border-t"
      >
        <CollapsibleTrigger className="flex items-center gap-2 text-sm font-medium hover:text-foreground transition-colors">
          <ChevronDown
            className={`h-4 w-4 transition-transform ${isAdvancedOpen ? "" : "-rotate-90"}`}
            aria-hidden="true"
          />
          Advanced
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="mt-4 p-4 border rounded-lg space-y-4">
            <div className="flex items-center gap-2">
              <h3 className="text-base font-medium">
                Attach Selected Environment
              </h3>
              {attachedEnvForm.selectedEnvironment && (
                <Badge variant="outline">
                  {attachedEnvForm.selectedEnvironment.slug}
                </Badge>
              )}
            </div>

            <p className="text-sm text-muted-foreground">
              Attaching an environment at the toolset level will automatically
              apply these environment variables to all users of tools from this
              toolset. This can be useful when the toolset requires
              configuration that should remain hidden to users.
            </p>

            <div className="flex items-start gap-2 px-4 py-3 bg-warning/10 border border-warning/20 rounded-md">
              <TriangleAlert
                className="h-4 w-4 text-warning flex-shrink-0 mt-0.5"
                aria-hidden="true"
              />
              <p className="text-sm text-warning">
                Environments attached here will apply to all users in both
                public and private servers
              </p>
            </div>

            <div>
              <Button
                type="button"
                size="sm"
                onClick={() => attachedEnvForm.mutation.mutate()}
                disabled={
                  !attachedEnvForm.isDirty || attachedEnvForm.mutation.isPending
                }
                aria-label={
                  attachedEnvForm.mutation.isPending
                    ? "Attaching environment to toolset"
                    : "Attach environment to toolset"
                }
              >
                {attachedEnvForm.mutation.isPending ? "Attaching..." : "Attach"}
              </Button>
            </div>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
