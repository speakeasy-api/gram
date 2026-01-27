import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@speakeasy-api/moonshine";
import { Plus, Trash2 } from "lucide-react";
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
  hasExistingConfigs: boolean;
  onEnvironmentSelect: (slug: string) => void;
  onSaveAll: () => void;
  onCancelAll: () => void;
  onSetDefaultEnvironment: () => void;
  onCreateEnvironment: () => void;
  onDeleteEnvironment: () => void;
}

export function EnvironmentSwitcher({
  environments,
  selectedEnvironmentView,
  mcpAttachedEnvironmentSlug,
  defaultEnvironmentSlug,
  requiredVars,
  hasAnyUserEdits,
  hasExistingConfigs,
  onEnvironmentSelect,
  onSaveAll,
  onCancelAll,
  onSetDefaultEnvironment,
  onCreateEnvironment,
  onDeleteEnvironment,
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
  const hasAllRequired = environmentHasAllRequiredVariables(
    attachedEnvSlug,
    requiredVars,
  );

  if (environments.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 border-b">
      {/* Environment tabs */}
      {sortedEnvironments.map((env) => {
        const isSelected = selectedEnvironmentView === env.slug;
        const envHasAllRequired = environmentHasAllRequiredVariables(
          env.slug,
          requiredVars,
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
            <div
              className={cn(
                "w-2 h-2 rounded-full",
                envHasAllRequired ? "bg-green-500" : "bg-yellow-500",
              )}
            />
            {getEnvironmentDisplayName(env)}
            {isSelected && (
              <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary" />
            )}
          </button>
        );
      })}

      {/* Right side actions */}
      <div className="ml-auto flex items-center gap-3 py-1.5 pr-3">
        {/* Status/action area */}
        {hasAnyUserEdits ? (
          <>
            {/* Unsaved changes indicator */}
            <span className="text-xs text-amber-600 font-medium flex items-center gap-1.5">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
              Unsaved changes
            </span>

            {/* Cancel button - only for existing configs */}
            {hasExistingConfigs && (
              <Button onClick={onCancelAll} variant="tertiary" size="xs">
                <Button.Text>Cancel</Button.Text>
              </Button>
            )}

            {/* Save button */}
            <Button onClick={onSaveAll} variant="primary" size="xs">
              <Button.Text>
                {hasExistingConfigs ? "Save" : "Save Configuration"}
              </Button.Text>
            </Button>
          </>
        ) : (
          <>
            {/* When no edits - show contextual actions */}
            {!hasExistingConfigs && !hasAllRequired ? (
              // Initial setup: prompt to fill required fields
              <span className="text-xs text-muted-foreground">
                Fill in required values to save
              </span>
            ) : isViewingNonDefault ? (
              // Viewing non-default env - show Make Default and Delete
              <>
                <SimpleTooltip tooltip="Set this as the default environment for this MCP server">
                  <Button
                    onClick={onSetDefaultEnvironment}
                    variant="secondary"
                    size="xs"
                  >
                    <Button.Text>Make Default</Button.Text>
                  </Button>
                </SimpleTooltip>
                <SimpleTooltip tooltip="Delete this environment">
                  <Button
                    onClick={onDeleteEnvironment}
                    variant="destructive-secondary"
                    size="xs"
                  >
                    <Button.LeftIcon>
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button.LeftIcon>
                    <Button.Text>Delete</Button.Text>
                  </Button>
                </SimpleTooltip>
              </>
            ) : hasExistingConfigs ? (
              // Has configs, on default env - show new environment option
              <SimpleTooltip tooltip="Create a new environment with different values">
                <Button
                  onClick={onCreateEnvironment}
                  variant="secondary"
                  size="xs"
                >
                  <Button.LeftIcon>
                    <Plus className="w-3.5 h-3.5" />
                  </Button.LeftIcon>
                  <Button.Text>New Environment</Button.Text>
                </Button>
              </SimpleTooltip>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
