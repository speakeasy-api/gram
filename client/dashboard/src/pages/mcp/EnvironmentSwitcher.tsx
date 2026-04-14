import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@speakeasy-api/moonshine";
import { Plus, Unlink } from "lucide-react";
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
  onDetachEnvironment: () => void;
  onCreateEnvironment: () => void;
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
  onDetachEnvironment,
  onCreateEnvironment,
}: EnvironmentSwitcherProps) {
  // Sort environments with attached environment first, falling back to default for sort order only
  const sortSlug =
    mcpAttachedEnvironmentSlug || defaultEnvironmentSlug || "default";
  const sortedEnvironments = [...environments].sort((a, b) => {
    if (a.slug === sortSlug) return -1;
    if (b.slug === sortSlug) return 1;
    return 0;
  });

  // Helper to get display name for environment
  const getEnvironmentDisplayName = (env: Environment) => {
    if (env.slug === "default") return "Default";
    return env.name;
  };

  const isViewingNonAttached = mcpAttachedEnvironmentSlug
    ? selectedEnvironmentView !== mcpAttachedEnvironmentSlug
    : true; // If nothing is attached, every environment is "non-attached"
  const hasAllRequired = mcpAttachedEnvironmentSlug
    ? environmentHasAllRequiredVariables(
        mcpAttachedEnvironmentSlug,
        requiredVars,
      )
    : false;

  if (environments.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 border-b">
      {/* Environment tabs */}
      {sortedEnvironments.map((env) => {
        const isSelected = selectedEnvironmentView === env.slug;
        const isAttachedEnv =
          mcpAttachedEnvironmentSlug != null &&
          env.slug === mcpAttachedEnvironmentSlug;
        const envHasAllRequired = environmentHasAllRequiredVariables(
          env.slug,
          requiredVars,
        );

        return (
          <button
            key={env.slug}
            onClick={() => onEnvironmentSelect(env.slug)}
            className={cn(
              "relative flex items-center gap-2 px-4 py-2.5 text-sm font-medium transition-colors",
              isSelected
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {isAttachedEnv && (
              <div
                className={cn(
                  "h-2 w-2 rounded-full",
                  envHasAllRequired ? "bg-green-500" : "bg-yellow-500",
                )}
              />
            )}
            {getEnvironmentDisplayName(env)}
            {isSelected && (
              <div className="bg-primary absolute right-0 bottom-0 left-0 h-0.5" />
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
            <span className="flex items-center gap-1.5 text-xs font-medium text-amber-600">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500" />
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
              <span className="text-muted-foreground text-xs">
                Fill in required values to save
              </span>
            ) : isViewingNonAttached ? (
              // Viewing non-attached env - show Attach
              <SimpleTooltip tooltip="Attach this environment to this MCP server">
                <Button
                  onClick={onSetDefaultEnvironment}
                  variant="secondary"
                  size="xs"
                >
                  <Button.Text>Attach</Button.Text>
                </Button>
              </SimpleTooltip>
            ) : hasExistingConfigs ? (
              // Has configs, on attached env - show new environment and detach options
              <>
                <SimpleTooltip tooltip="Create a new environment with different values">
                  <Button
                    onClick={onCreateEnvironment}
                    variant="secondary"
                    size="xs"
                  >
                    <Button.LeftIcon>
                      <Plus className="h-3.5 w-3.5" />
                    </Button.LeftIcon>
                    <Button.Text>New Environment</Button.Text>
                  </Button>
                </SimpleTooltip>
                <SimpleTooltip tooltip="Detach this environment from this MCP server">
                  <Button
                    onClick={onDetachEnvironment}
                    variant="tertiary"
                    size="xs"
                  >
                    <Button.LeftIcon>
                      <Unlink className="h-3.5 w-3.5" />
                    </Button.LeftIcon>
                    <Button.Text>Detach</Button.Text>
                  </Button>
                </SimpleTooltip>
              </>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
