import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@speakeasy-api/moonshine";
import { AlertTriangle, CheckCircleIcon, Plus } from "lucide-react";
import {
  EnvironmentVariable,
  environmentHasAllRequiredVariables,
} from "./environmentVariableUtils";

interface Environment {
  id: string;
  slug: string;
  name: string;
}

interface EnvironmentSwitcherProps {
  environments: Environment[];
  selectedEnvironmentView: string;
  mcpAttachedEnvironmentSlug: string | null;
  defaultEnvironmentSlug: string;
  requiredVars: EnvironmentVariable[];
  hasAnyUserEdits: boolean;
  hasAnyUnsavedChanges: boolean;
  hasExistingConfigs: boolean;
  onEnvironmentSelect: (slug: string) => void;
  onSaveAll: () => void;
  onCancelAll: () => void;
  onSetDefaultEnvironment: () => void;
  onCreateEnvironment: () => void;
}

export function EnvironmentSwitcher({
  environments,
  selectedEnvironmentView,
  mcpAttachedEnvironmentSlug,
  defaultEnvironmentSlug,
  requiredVars,
  hasAnyUserEdits,
  hasAnyUnsavedChanges,
  hasExistingConfigs,
  onEnvironmentSelect,
  onSaveAll,
  onCancelAll,
  onSetDefaultEnvironment,
  onCreateEnvironment,
}: EnvironmentSwitcherProps) {
  // Sort environments with attached environment first
  const attachedEnvSlug =
    mcpAttachedEnvironmentSlug || defaultEnvironmentSlug || "default";
  const sortedEnvironments = [...environments].sort((a, b) => {
    if (a.slug === attachedEnvSlug) return -1;
    if (b.slug === attachedEnvSlug) return 1;
    return 0;
  });

  // Helper to get display name for environment
  const getEnvironmentDisplayName = (env: Environment) => {
    if (env.slug === "default") return "Project";
    return env.name;
  };

  const isViewingNonDefault = selectedEnvironmentView !== attachedEnvSlug;

  if (environments.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 border-b">
      {sortedEnvironments.map((env) => {
        const isSelected = selectedEnvironmentView === env.slug;
        const isAttachedEnvironment = env.slug === attachedEnvSlug;
        const hasAllRequired = environmentHasAllRequiredVariables(
          env.slug,
          requiredVars,
        );
        const icon = hasAllRequired ? (
          <CheckCircleIcon className="w-4 h-4 text-green-600" />
        ) : (
          <AlertTriangle className="w-4 h-4 text-yellow-600" />
        );

        return (
          <button
            key={env.slug}
            onClick={() => onEnvironmentSelect(env.slug)}
            className={cn(
              "flex items-center gap-2 px-4 py-2.5 text-sm font-medium transition-colors relative",
              isSelected
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {isAttachedEnvironment && icon}
            {getEnvironmentDisplayName(env)}
            {isSelected && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary" />
            )}
          </button>
        );
      })}
      <div className="ml-auto flex items-center gap-2 mr-1">
        {/* Unsaved changes indicator - only show when user has made actual edits */}
        {hasAnyUserEdits && (
          <span className="text-xs text-amber-600 font-medium flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
            Unsaved changes
          </span>
        )}

        {/* Cancel button - only shown when there are changes and existing configs */}
        {hasAnyUnsavedChanges && hasExistingConfigs && (
          <Button onClick={onCancelAll} variant="tertiary" size="xs">
            <Button.Text>Cancel</Button.Text>
          </Button>
        )}

        {/* Save button - always visible, disabled when no changes */}
        <SimpleTooltip
          tooltip={
            hasAnyUnsavedChanges
              ? "Save all environment variable changes"
              : "No changes to save"
          }
        >
          <Button
            onClick={onSaveAll}
            variant={hasAnyUnsavedChanges ? "primary" : "secondary"}
            size="xs"
            className="mr-4"
            disabled={!hasAnyUnsavedChanges}
          >
            <Button.Text>
              {hasExistingConfigs ? "Save All" : "Publish Configuration"}
            </Button.Text>
          </Button>
        </SimpleTooltip>

        {/* Additional actions when no unsaved changes */}
        {!hasAnyUnsavedChanges && isViewingNonDefault && (
          <SimpleTooltip tooltip="Set this as the default environment for this toolset. Non-user-provided variables will be sourced from this environment">
            <Button
              onClick={onSetDefaultEnvironment}
              variant="tertiary"
              size="xs"
            >
              <Button.Text>Make Default</Button.Text>
            </Button>
          </SimpleTooltip>
        )}
        {!hasAnyUnsavedChanges && !isViewingNonDefault && (
          <SimpleTooltip tooltip="Create a new environment">
            <Button onClick={onCreateEnvironment} variant="tertiary" size="xs">
              <Button.Icon>
                <Plus className="w-4 h-4" />
              </Button.Icon>
              <Button.Text>New Environment</Button.Text>
            </Button>
          </SimpleTooltip>
        )}
      </div>
    </div>
  );
}
