import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/moonshine";
import { Check, ChevronsUpDown, Link, Plus, Unlink } from "lucide-react";
import { useState } from "react";
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
}: EnvironmentSwitcherProps): JSX.Element | null {
  const [pickerOpen, setPickerOpen] = useState(false);

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

  // Dropdown row indicator: a chain-link icon (with an "Attached" tooltip) on
  // the attached env, transparent spacer of the same size on the rest so
  // labels stay aligned.
  const renderAttachedIndicator = (env: Environment) => {
    const isAttachedEnv =
      mcpAttachedEnvironmentSlug != null &&
      env.slug === mcpAttachedEnvironmentSlug;
    if (!isAttachedEnv) {
      return <div className="h-3 w-3 shrink-0" />;
    }
    return (
      <SimpleTooltip tooltip="Attached">
        <Link className="text-foreground h-3 w-3 shrink-0" />
      </SimpleTooltip>
    );
  };

  const selectedEnv = sortedEnvironments.find(
    (env) => env.slug === selectedEnvironmentView,
  );
  const selectedLabel = selectedEnv
    ? getEnvironmentDisplayName(selectedEnv)
    : selectedEnvironmentView;
  const selectedIsAttached =
    mcpAttachedEnvironmentSlug != null &&
    selectedEnvironmentView === mcpAttachedEnvironmentSlug;

  if (environments.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 border-b">
      {/* Environment selector */}
      <div className="py-2 pl-3">
        <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="secondary"
              role="combobox"
              aria-expanded={pickerOpen}
              className="min-w-[200px] justify-between gap-2 px-3"
            >
              <Button.Text className="flex items-center gap-2 truncate">
                {selectedIsAttached && (
                  <SimpleTooltip tooltip="Attached">
                    <Link className="text-foreground h-3 w-3 shrink-0" />
                  </SimpleTooltip>
                )}
                <span className="truncate">{selectedLabel}</span>
              </Button.Text>
              <Button.RightIcon>
                <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
              </Button.RightIcon>
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[280px] p-0" align="start">
            <Command>
              {sortedEnvironments.length > 4 && (
                <CommandInput
                  placeholder="Search environments..."
                  className="h-9"
                />
              )}
              <CommandList>
                <CommandEmpty>No environments found.</CommandEmpty>
                <CommandGroup>
                  {sortedEnvironments.map((env) => {
                    const isSelected = selectedEnvironmentView === env.slug;
                    return (
                      <CommandItem
                        key={env.slug}
                        value={env.slug}
                        keywords={[getEnvironmentDisplayName(env)]}
                        className="cursor-pointer gap-2"
                        onSelect={(slug) => {
                          onEnvironmentSelect(slug);
                          setPickerOpen(false);
                        }}
                      >
                        {renderAttachedIndicator(env)}
                        <span className="truncate">
                          {getEnvironmentDisplayName(env)}
                        </span>
                        <Check
                          className={cn(
                            "ml-auto h-4 w-4 shrink-0",
                            isSelected ? "opacity-100" : "opacity-0",
                          )}
                        />
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>

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
