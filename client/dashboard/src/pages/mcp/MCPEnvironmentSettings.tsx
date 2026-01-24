import { Combobox } from "@/components/ui/combobox";
import { InputAndMultiselect } from "@/components/ui/InputAndMultiselect";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import {
  invalidateAllListEnvironments,
  useListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CheckCircleIcon,
  ChevronDown,
  Eye,
  EyeOff,
  Plus,
  Trash2,
  XCircleIcon
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

/**
 * Environment Variable type for the Environments tab
 */
interface EnvironmentVariable {
  id: string;
  key: string;
  // Track multiple values per variable - each value can be in different environments
  valueGroups: Array<{
    valueHash: string;
    value: string; // Redacted value for display
    environments: string[]; // Environment slugs that have this value
  }>;
  isUserProvided: boolean;
  isRequired: boolean; // True for advertised vars from toolset, false for custom user-added
  description?: string; // Optional description for required vars
  createdAt?: Date;
  updatedAt?: Date;
}

/**
 * Environments Tab - Vercel-style environment variables management
 */
export function MCPAuthenticationTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  // State for the list of environment variables
  const [envVars, setEnvVars] = useState<EnvironmentVariable[]>([]);
  const [isAddingNew, setIsAddingNew] = useState(false);
  const [selectedEnvironmentView, setSelectedEnvironmentView] = useState<string | null>(null);

  // Initialize selectedEnvironmentView to default environment when environments load
  useEffect(() => {
    if (environments.length > 0 && !selectedEnvironmentView && toolset.defaultEnvironmentSlug) {
      const defaultEnv = environments.find(e => e.slug === toolset.defaultEnvironmentSlug);
      if (defaultEnv) {
        setSelectedEnvironmentView(toolset.defaultEnvironmentSlug);
      }
    }
  }, [environments, toolset.defaultEnvironmentSlug, selectedEnvironmentView]);

  // Clear editing state when environment view changes
  useEffect(() => {
    setEditingState(new Map());
  }, [selectedEnvironmentView]);

  // New variable form state
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [newTargetEnvironments, setNewTargetEnvironments] = useState<string[]>(
    [],
  );
  const [newIsUserProvided, setNewIsUserProvided] = useState(false);
  const [newValueVisible, setNewValueVisible] = useState(false);

  // Edit variable state
  const [editingVar, setEditingVar] = useState<EnvironmentVariable | null>(
    null,
  );
  const [editValue, setEditValue] = useState("");
  const [editTargetEnvironments, setEditTargetEnvironments] = useState<
    string[]
  >([]);
  const [editValueVisible, setEditValueVisible] = useState(false);

  // Track editing state for required variables (value and target environments)
  type EditingState = { value: string; targetEnvironments: string[] };
  const [editingState, setEditingState] = useState<Map<string, EditingState>>(
    new Map(),
  );

  // Update environment mutation
  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      invalidateAllListEnvironments(queryClient);
      telemetry.capture("environment_event", {
        action: "environment_variable_updated",
        toolset_slug: toolset.slug,
      });
    },
  });

  // Load existing environment variables from toolset
  useEffect(() => {
    const existingVars: EnvironmentVariable[] = [];
    const envMap = new Map<string, string[]>();
    const requiredVarNames = new Set<string>();

    // Helper to build value groups for a variable across all environments
    const getValueGroups = (varName: string) => {
      const valueHashMap = new Map<
        string,
        { value: string; environments: string[] }
      >();

      environments.forEach((env) => {
        const entry = env.entries.find((e) => e.name === varName);
        if (entry) {
          if (!valueHashMap.has(entry.valueHash)) {
            valueHashMap.set(entry.valueHash, {
              value: entry.value,
              environments: [env.slug],
            });
          } else {
            valueHashMap.get(entry.valueHash)!.environments.push(env.slug);
          }
        }
      });

      return Array.from(valueHashMap.entries()).map(
        ([valueHash, { value, environments }]) => ({
          valueHash,
          value,
          environments,
        }),
      );
    };

    // Get env vars from security variables (these are required auth credentials)
    toolset.securityVariables?.forEach((secVar) => {
      secVar.envVariables.forEach((envVar) => {
        if (!envVar.toLowerCase().includes("token_url")) {
          requiredVarNames.add(envVar);
          const valueGroups = getValueGroups(envVar);
          const id = `sec-${secVar.id}-${envVar}`;
          existingVars.push({
            id,
            key: envVar,
            valueGroups,
            isUserProvided: true,
            isRequired: true,
            description: `Authentication credential for ${secVar.name || "API access"}`,
            createdAt: new Date(),
          });
          // Initialize the environments map with the most common value's environments
          if (valueGroups.length > 0) {
            const mostCommonGroup = valueGroups.reduce((prev, current) =>
              current.environments.length > prev.environments.length
                ? current
                : prev,
            );
            envMap.set(id, mostCommonGroup.environments);
          }
        }
      });
    });

    // Get env vars from server variables (these are required server config)
    toolset.serverVariables?.forEach((serverVar) => {
      serverVar.envVariables.forEach((envVar) => {
        requiredVarNames.add(envVar);
        const valueGroups = getValueGroups(envVar);
        const id = `srv-${envVar}`;
        existingVars.push({
          id,
          key: envVar,
          valueGroups,
          isUserProvided: false,
          isRequired: true,
          description: "Server configuration variable",
          createdAt: new Date(),
        });
        // Initialize the environments map with the most common value's environments
        if (valueGroups.length > 0) {
          const mostCommonGroup = valueGroups.reduce((prev, current) =>
            current.environments.length > prev.environments.length
              ? current
              : prev,
          );
          envMap.set(id, mostCommonGroup.environments);
        }
      });
    });

    // Get env vars from function environment variables (these are required for functions)
    toolset.functionEnvironmentVariables?.forEach((funcVar) => {
      requiredVarNames.add(funcVar.name);
      const valueGroups = getValueGroups(funcVar.name);
      const id = `func-${funcVar.name}`;
      existingVars.push({
        id,
        key: funcVar.name,
        valueGroups,
        isUserProvided: false,
        isRequired: true,
        description: funcVar.description || "Function environment variable",
        createdAt: new Date(),
      });
      // Initialize the environments map with the most common value's environments
      if (valueGroups.length > 0) {
        const mostCommonGroup = valueGroups.reduce((prev, current) =>
          current.environments.length > prev.environments.length
            ? current
            : prev,
        );
        envMap.set(id, mostCommonGroup.environments);
      }
    });

    // Load custom variables from environments (variables not in the required list)
    const customVarMap = new Map<
      string,
      {
        valueGroups: Map<string, { value: string; environments: Set<string> }>;
        createdAt: Date;
      }
    >();

    environments.forEach((env) => {
      env.entries.forEach((entry) => {
        // Skip if this is a required variable or a token_url
        if (
          !requiredVarNames.has(entry.name) &&
          !entry.name.toLowerCase().includes("token_url")
        ) {
          if (!customVarMap.has(entry.name)) {
            customVarMap.set(entry.name, {
              valueGroups: new Map([
                [
                  entry.valueHash,
                  { value: entry.value, environments: new Set([env.slug]) },
                ],
              ]),
              createdAt: entry.createdAt,
            });
          } else {
            const varData = customVarMap.get(entry.name)!;
            if (!varData.valueGroups.has(entry.valueHash)) {
              varData.valueGroups.set(entry.valueHash, {
                value: entry.value,
                environments: new Set([env.slug]),
              });
            } else {
              varData.valueGroups.get(entry.valueHash)!.environments.add(env.slug);
            }
          }
        }
      });
    });

    // Add custom variables to the list
    customVarMap.forEach((info, varName) => {
      const id = `custom-${varName}`;
      const valueGroups = Array.from(info.valueGroups.entries()).map(
        ([valueHash, { value, environments }]) => ({
          valueHash,
          value,
          environments: Array.from(environments),
        }),
      );
      existingVars.push({
        id,
        key: varName,
        valueGroups,
        isUserProvided: false,
        isRequired: false,
        description: "Custom environment variable",
        createdAt: info.createdAt,
      });
    });

    setEnvVars(existingVars);
  }, [toolset.slug, environments]);

  const handleAddVariable = () => {
    if (!newKey.trim()) return;

    // If no environments are explicitly selected, use all environments
    const targetEnvs =
      newTargetEnvironments.length > 0
        ? newTargetEnvironments
        : environments.map((e) => e.slug);

    // Save to selected environments
    // Don't add to envVars state - it will be reloaded from environments after save
    const varKey = newKey.toUpperCase().replace(/\s+/g, "_");
    if (!newIsUserProvided && newValue && targetEnvs.length > 0) {
      targetEnvs.forEach((envSlug) => {
        updateEnvironmentMutation.mutate({
          request: {
            slug: envSlug,
            updateEnvironmentRequestBody: {
              entriesToUpdate: [{ name: varKey, value: newValue }],
              entriesToRemove: [],
            },
          },
        });
      });
    }
    setNewKey("");
    setNewValue("");
    setNewTargetEnvironments([]);
    setNewIsUserProvided(false);
    setNewValueVisible(false);
    setIsAddingNew(false);

    telemetry.capture("environment_event", {
      action: "environment_variable_added",
      toolset_slug: toolset.slug,
      is_user_provided: newIsUserProvided,
    });
  };

  const handleDeleteVariable = (id: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Delete from all environments that have this variable
    const allEnvs = getAllEnvironments(envVar);
    allEnvs.forEach((envSlug) => {
      updateEnvironmentMutation.mutate({
        request: {
          slug: envSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [],
            entriesToRemove: [envVar.key],
          },
        },
      });
    });

    telemetry.capture("environment_event", {
      action: "environment_variable_deleted",
      toolset_slug: toolset.slug,
    });
  };

  // Helper functions for working with valueGroups
  const getAllEnvironments = (envVar: EnvironmentVariable): string[] => {
    const allEnvs = new Set<string>();
    envVar.valueGroups.forEach((group) => {
      group.environments.forEach((env) => allEnvs.add(env));
    });
    return Array.from(allEnvs);
  };

  const getPrimaryValue = (envVar: EnvironmentVariable): string => {
    if (envVar.valueGroups.length === 0) return "";
    // Return the value from the most common group
    const mostCommonGroup = envVar.valueGroups.reduce((prev, current) =>
      current.environments.length > prev.environments.length ? current : prev,
    );
    return mostCommonGroup.value;
  };

  const hasValue = (envVar: EnvironmentVariable): boolean => {
    return envVar.valueGroups.length > 0 && envVar.valueGroups.some(g => g.environments.length > 0);
  };

  // Check if an environment has a value for a specific variable
  const environmentHasValue = (envVar: EnvironmentVariable, environmentSlug: string): boolean => {
    if (envVar.isUserProvided) return true;
    return envVar.valueGroups.some(group => group.environments.includes(environmentSlug));
  };

  // Get the value for a variable in a specific environment
  const getValueForEnvironment = (envVar: EnvironmentVariable, environmentSlug: string): string => {
    const group = envVar.valueGroups.find(g => g.environments.includes(environmentSlug));
    return group?.value || "";
  };

  // Separate required and custom variables
  const requiredVars = envVars.filter((v) => v.isRequired);
  const customVars = envVars.filter((v) => !v.isRequired);

  // Check if an environment has all required variables configured
  const environmentHasAllRequiredVariables = (environmentSlug: string): boolean => {
    return requiredVars.every(v => environmentHasValue(v, environmentSlug));
  };

  // Count missing required variables (user-provided ones count as configured)
  const missingRequiredCount = requiredVars.filter(
    (v) => !hasValue(v) && !v.isUserProvided,
  ).length;

  // Handle value change for required variables
  const handleValueChange = (id: string, newValue: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Get current or default target environments
    let targetEnvironments: string[];
    if (editingState.has(id)) {
      targetEnvironments = editingState.get(id)!.targetEnvironments;
    } else {
      // Check if the currently viewed environment has a value
      const currentEnvHasValue = selectedEnvironmentView
        ? envVar.valueGroups.some(g => g.environments.includes(selectedEnvironmentView))
        : false;

      // If viewing a specific environment and it doesn't have a value, default to all environments
      if (selectedEnvironmentView && !currentEnvHasValue) {
        targetEnvironments = environments.map((e) => e.slug);
      } else if (envVar.valueGroups.length > 0) {
        // If variable has values, use the most common group
        const mostCommonGroup = envVar.valueGroups.reduce((prev, current) =>
          current.environments.length > prev.environments.length
            ? current
            : prev,
        );
        targetEnvironments = mostCommonGroup.environments;
      } else {
        // If completely unset, use all environments
        targetEnvironments = environments.map((e) => e.slug);
      }
    }

    setEditingState(
      new Map(editingState.set(id, { value: newValue, targetEnvironments })),
    );
  };

  // Get editing value for a variable (either from editing state or from valueGroups)
  const getEditingValue = (envVar: EnvironmentVariable): string => {
    if (editingState.has(envVar.id)) {
      return editingState.get(envVar.id)!.value;
    }
    // If viewing a specific environment, show that environment's value
    if (selectedEnvironmentView) {
      return getValueForEnvironment(envVar, selectedEnvironmentView);
    }
    return getPrimaryValue(envVar);
  };

  // Get selected environments for a variable
  const getSelectedEnvironments = (id: string): string[] => {
    // If actively editing, use the editing state
    if (editingState.has(id)) {
      return editingState.get(id)!.targetEnvironments;
    }

    // Otherwise, show which environments have the currently displayed value
    const envVar = envVars.find(v => v.id === id);
    if (!envVar) return environments.map((e) => e.slug);

    // If viewing a specific environment, find which environments have that same value
    if (selectedEnvironmentView) {
      const viewedValue = getValueForEnvironment(envVar, selectedEnvironmentView);
      if (viewedValue) {
        const matchingGroup = envVar.valueGroups.find(g => g.value === viewedValue);
        if (matchingGroup) {
          return matchingGroup.environments;
        }
      }
      // If the current environment has no value, default to all environments
      return environments.map((e) => e.slug);
    }

    // Default to showing environments with the primary value
    const primaryValue = getPrimaryValue(envVar);
    if (primaryValue) {
      const matchingGroup = envVar.valueGroups.find(g => g.value === primaryValue);
      if (matchingGroup) {
        return matchingGroup.environments;
      }
    }

    return environments.map((e) => e.slug);
  };

  // Update selected environments for a variable
  const setSelectedEnvironments = (id: string, envs: string[]) => {
    const current = editingState.get(id);
    if (current) {
      setEditingState(
        new Map(editingState.set(id, { ...current, targetEnvironments: envs })),
      );
    } else {
      // Initialize editing state with current value and new environments
      const envVar = envVars.find(v => v.id === id);
      const value = envVar ? getEditingValue(envVar) : "";
      setEditingState(
        new Map(editingState.set(id, { value, targetEnvironments: envs })),
      );
    }
  };

  // Get environments with different values (for indeterminate checkbox state)
  const getIndeterminateEnvironments = (id: string): string[] => {
    const envVar = envVars.find(v => v.id === id);
    if (!envVar) return [];

    const currentValue = getEditingValue(envVar);
    const selectedEnvs = getSelectedEnvironments(id);

    // Find environments that have a value different from the current value
    // and are not already selected
    return environments
      .filter(env => {
        // Skip if already selected
        if (selectedEnvs.includes(env.slug)) return false;

        // Get the value for this environment
        const envValue = getValueForEnvironment(envVar, env.slug);

        // Include if this environment has a value and it's different from current
        return envValue && envValue !== currentValue;
      })
      .map(env => env.slug);
  };

  // Toggle user-provided state for a variable
  const handleToggleUserProvided = (id: string) => {
    setEnvVars(
      envVars.map((v) =>
        v.id === id ? { ...v, isUserProvided: !v.isUserProvided } : v,
      ),
    );
    // Clear editing state when toggling
    const newEditingState = new Map(editingState);
    newEditingState.delete(id);
    setEditingState(newEditingState);
  };

  // Save a required variable
  const handleSaveVariable = (envVar: EnvironmentVariable) => {
    const value = getEditingValue(envVar);
    if (!value) return;

    // Use selected environments from state
    const targetEnvs = getSelectedEnvironments(envVar.id);

    if (targetEnvs.length === 0) {
      toast.error("No environments selected");
      return;
    }

    targetEnvs.forEach((envSlug) => {
      updateEnvironmentMutation.mutate({
        request: {
          slug: envSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [{ name: envVar.key, value }],
            entriesToRemove: [],
          },
        },
      });
    });

    // Clear editing state after save
    const newEditingState = new Map(editingState);
    newEditingState.delete(envVar.id);
    setEditingState(newEditingState);

    toast.success(`Saved ${envVar.key} to ${targetEnvs.length} environment${targetEnvs.length > 1 ? "s" : ""}`);

    telemetry.capture("environment_event", {
      action: "required_variable_configured",
      toolset_slug: toolset.slug,
      variable_key: envVar.key,
    });
  };

  const environmentSwitcher = useMemo(() => {
    return environments.length > 0 && (
      <Combobox
        items={environments.map(env => ({
          value: env.slug,
          label: env.name,
          icon: environmentHasAllRequiredVariables(env.slug) ? (
            <CheckCircleIcon className="w-4 h-4 text-green-600 mr-2" />
          ) : (
            <XCircleIcon className="w-4 h-4 text-muted-foreground/50 mr-2" />
          ),
        }))}
        selected={selectedEnvironmentView || undefined}
        onSelectionChange={(item) => setSelectedEnvironmentView(item.value)}
        variant="outline"
        className="min-w-[200px]"
      >
        <Type variant="small">
          {environments.find(e => e.slug === selectedEnvironmentView)?.name || "Select Environment"}
        </Type>
      </Combobox>
    )
  }, [environments, selectedEnvironmentView, requiredVars, envVars]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">
            Environment Variables
          </h2>
          <p className="text-sm text-muted-foreground mt-1">
            Configure required credentials and add custom variables.
          </p>
        </div>
       {environmentSwitcher}
      </div>

      {/* All Variables Section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h3 className="text-lg font-medium">Variables</h3>
            {missingRequiredCount > 0 && (
              <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs text-muted-foreground">
                {missingRequiredCount} required not configured
              </span>
            )}
          </div>
          <Button onClick={() => setIsAddingNew(true)} disabled={isAddingNew}>
            <Button.Text>Add Variable</Button.Text>
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">
          Configure environment variables for this MCP server. Required variables are indicated with a warning dot when unset.
        </p>

        {/* Variables List */}
        {envVars.length > 0 ? (
          <div className="border rounded-lg overflow-hidden">
            {envVars.map((envVar, index) => (
              <div
                key={envVar.id}
                className={cn(
                  "group grid grid-cols-[auto_1fr_auto] gap-4 items-center px-5 py-4 transition-colors",
                  index !== envVars.length - 1 && "border-b",
                )}
              >
                {/* Status indicator / Delete button - status shows by default, delete button replaces it on hover for non-required */}
                <div className="relative w-6 h-6 flex items-center justify-center">
                  {/* Status indicator - visible by default, hidden on hover for non-required */}
                  <div className={cn(!envVar.isRequired && "group-hover:opacity-0 transition-opacity")}>
                    {selectedEnvironmentView
                      ? (environmentHasValue(envVar, selectedEnvironmentView) ? (
                          <div className="w-2 h-2 rounded-full bg-green-500" />
                        ) : envVar.isRequired ? (
                          <div className="w-2 h-2 rounded-full bg-yellow-500" />
                        ) : (
                          <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
                        ))
                      : (hasValue(envVar) || envVar.isUserProvided ? (
                          <div className="w-2 h-2 rounded-full bg-green-500" />
                        ) : envVar.isRequired ? (
                          <div className="w-2 h-2 rounded-full bg-yellow-500" />
                        ) : (
                          <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
                        ))
                    }
                  </div>

                  {/* Delete button - hidden by default, visible on hover for non-required */}
                  {!envVar.isRequired && (
                    <button
                      onClick={() => handleDeleteVariable(envVar.id)}
                      className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center text-muted-foreground hover:text-destructive"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>

                {/* Variable Info */}
                <div className="min-w-0">
                  <div className="font-medium font-mono text-sm truncate">
                    {envVar.key}
                  </div>
                  {envVar.description && (
                    <div className="text-xs text-muted-foreground mt-0.5 truncate">
                      {envVar.description}
                    </div>
                  )}
                </div>

                {/* Right side: Toggle + Value + Environments + Save */}
                <div className="flex items-center gap-4">
                  {/* User provided toggle */}
                  <label className="flex items-center gap-2 cursor-pointer">
                    <Switch
                      checked={envVar.isUserProvided}
                      onCheckedChange={() => handleToggleUserProvided(envVar.id)}
                    />
                    <span className="text-xs text-muted-foreground whitespace-nowrap">
                      User provided
                    </span>
                  </label>

                  {/* Value Input or Runtime badge with dropdown */}
                  <div className="w-56">
                    {envVar.isUserProvided ? (
                      <div className="h-9 flex items-center px-3 rounded-md bg-muted text-xs text-muted-foreground font-mono">
                        Set at runtime
                      </div>
                    ) : (
                      <InputAndMultiselect
                        value={getEditingValue(envVar)}
                        onChange={(value) => handleValueChange(envVar.id, value)}
                        selectedOptions={getSelectedEnvironments(envVar.id)}
                        indeterminateOptions={getIndeterminateEnvironments(envVar.id)}
                        onSelectedOptionsChange={(selected) =>
                          setSelectedEnvironments(envVar.id, selected)
                        }
                        options={environments.map((env) => ({
                          value: env.slug,
                          label: env.name,
                        }))}
                        placeholder="Enter value..."
                        type="password"
                      />
                    )}
                  </div>

                  {/* Save button - always visible for consistent width */}
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => handleSaveVariable(envVar)}
                    disabled={!editingState.has(envVar.id) || !editingState.get(envVar.id)?.value || envVar.isUserProvided}
                    className={envVar.isUserProvided ? "invisible" : ""}
                  >
                    Save
                  </Button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          // Empty State
          <div className="border rounded-lg border-dashed p-8 text-center">
            <p className="text-muted-foreground mb-4">
              No environment variables added yet.
            </p>
            <Button onClick={() => setIsAddingNew(true)} variant="secondary">
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add Variable</Button.Text>
            </Button>
          </div>
        )}
      </div>

      {/* Add New Variable Sheet */}
      <Sheet open={isAddingNew} onOpenChange={setIsAddingNew}>
        <SheetContent
          side="right"
          className="w-[500px] sm:max-w-[500px] flex flex-col"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Add Environment Variable
            </SheetTitle>
          </SheetHeader>

          <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
            {/* Key and Value inputs side by side */}
            <div className="flex gap-4">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Key
                </Label>
                <input
                  type="text"
                  value={newKey}
                  onChange={(e) => setNewKey(e.target.value.toUpperCase())}
                  placeholder="CLIENT_KEY..."
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Value
                </Label>
                <input
                  type={newValueVisible ? "text" : "password"}
                  value={newValue}
                  onChange={(e) => setNewValue(e.target.value)}
                  placeholder=""
                  disabled={newIsUserProvided}
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed"
                />
              </div>
            </div>

            {/* Add Note link */}
            <button className="text-sm text-muted-foreground hover:text-foreground transition-colors">
              Add Note
            </button>

            {/* Add Another button */}
            <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <Plus className="h-4 w-4" />
              Add Another
            </button>

            {/* Environments section */}
            <div className="pt-4 border-t">
              <Label className="text-xs text-muted-foreground mb-2 block">
                Environments
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm flex items-center justify-between hover:bg-accent transition-colors">
                    <div className="flex items-center gap-2">
                      <svg
                        className="h-4 w-4 text-muted-foreground"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                        strokeWidth="2"
                      >
                        <rect x="3" y="3" width="18" height="6" rx="1" />
                        <rect
                          x="3"
                          y="11"
                          width="18"
                          height="6"
                          rx="1"
                          opacity="0.5"
                        />
                      </svg>
                      <span>
                        {newTargetEnvironments.length === 0 ||
                        newTargetEnvironments.length === environments.length
                          ? "All Environments"
                          : newTargetEnvironments.length === 1
                            ? environments.find(
                                (e) => e.slug === newTargetEnvironments[0],
                              )?.name || newTargetEnvironments[0]
                            : `${newTargetEnvironments.length} Environments`}
                      </span>
                    </div>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[352px] p-1">
                  <div
                    className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                    onClick={() =>
                      setNewTargetEnvironments(environments.map((e) => e.slug))
                    }
                  >
                    <div
                      className={cn(
                        "w-4 h-4 rounded-sm border flex items-center justify-center",
                        newTargetEnvironments.length === 0 ||
                          newTargetEnvironments.length === environments.length
                          ? "bg-primary border-primary text-primary-foreground"
                          : "border-border",
                      )}
                    >
                      {(newTargetEnvironments.length === 0 ||
                        newTargetEnvironments.length === environments.length) && (
                        <Check className="h-3 w-3" />
                      )}
                    </div>
                    All Environments
                  </div>
                  {environments.map((env) => (
                    <div
                      key={env.slug}
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => {
                        if (newTargetEnvironments.includes(env.slug)) {
                          setNewTargetEnvironments(
                            newTargetEnvironments.filter((s) => s !== env.slug),
                          );
                        } else {
                          setNewTargetEnvironments([
                            ...newTargetEnvironments,
                            env.slug,
                          ]);
                        }
                      }}
                    >
                      <div
                        className={cn(
                          "w-4 h-4 rounded-sm border flex items-center justify-center",
                          newTargetEnvironments.includes(env.slug)
                            ? "bg-primary border-primary text-primary-foreground"
                            : "border-border",
                        )}
                      >
                        {newTargetEnvironments.includes(env.slug) && (
                          <Check className="h-3 w-3" />
                        )}
                      </div>
                      {env.name}
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
            </div>

            {/* Sensitive toggle */}
            <div className="flex items-center justify-between pt-4">
              <div className="flex items-center gap-3">
                <Switch
                  checked={newIsUserProvided}
                  onCheckedChange={setNewIsUserProvided}
                />
                <div>
                  <span className="text-sm font-medium">Sensitive</span>
                  <span className="text-xs text-yellow-600 ml-2">⚡</span>
                  <p className="text-xs text-muted-foreground">
                    Available for Production and Preview only
                  </p>
                </div>
              </div>
            </div>
          </div>

          <SheetFooter className="px-6 py-4 border-t flex-row justify-between items-center">
            <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <svg
                className="h-4 w-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
                strokeWidth="2"
              >
                <path d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
              </svg>
              Import .env
            </button>
            <span className="text-xs text-muted-foreground">
              or paste .env contents in Key input
            </span>
            <Button
              onClick={() => {
                handleAddVariable();
                setIsAddingNew(false);
              }}
              disabled={!newKey.trim()}
            >
              Save
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Edit Variable Sheet */}
      <Sheet
        open={editingVar !== null}
        onOpenChange={(open) => {
          if (!open) {
            setEditingVar(null);
            setEditValue("");
            setEditTargetEnvironments([]);
            setEditValueVisible(false);
          }
        }}
      >
        <SheetContent
          side="right"
          className="w-[500px] sm:max-w-[500px] flex flex-col"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Edit Environment Variable
            </SheetTitle>
          </SheetHeader>

          <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
            {/* Key (read-only) and Value inputs side by side */}
            <div className="flex gap-4">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Key
                </Label>
                <input
                  type="text"
                  value={editingVar?.key || ""}
                  disabled
                  className="w-full h-10 px-3 rounded-md border border-input bg-muted text-sm font-mono cursor-not-allowed"
                />
              </div>
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Value
                </Label>
                <div className="relative">
                  <input
                    type={editValueVisible ? "text" : "password"}
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    placeholder="Enter value..."
                    disabled={editingVar?.isUserProvided}
                    className="w-full h-10 px-3 pr-10 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed"
                  />
                  <button
                    type="button"
                    onClick={() => setEditValueVisible(!editValueVisible)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    {editValueVisible ? (
                      <EyeOff className="h-4 w-4" />
                    ) : (
                      <Eye className="h-4 w-4" />
                    )}
                  </button>
                </div>
              </div>
            </div>

            {/* Environments section */}
            <div className="pt-4 border-t">
              <Label className="text-xs text-muted-foreground mb-2 block">
                Environments
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm flex items-center justify-between hover:bg-accent transition-colors">
                    <div className="flex items-center gap-2">
                      <svg
                        className="h-4 w-4 text-muted-foreground"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                        strokeWidth="2"
                      >
                        <rect x="3" y="3" width="18" height="6" rx="1" />
                        <rect
                          x="3"
                          y="11"
                          width="18"
                          height="6"
                          rx="1"
                          opacity="0.5"
                        />
                      </svg>
                      <span>
                        {editTargetEnvironments.length === 0
                          ? "Select Environments"
                          : editTargetEnvironments.length === environments.length
                            ? "All Environments"
                            : editTargetEnvironments.length === 1
                              ? environments.find(
                                  (e) => e.slug === editTargetEnvironments[0],
                                )?.name || editTargetEnvironments[0]
                              : `${editTargetEnvironments.length} Environments`}
                      </span>
                    </div>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[352px] p-1">
                  <div
                    className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                    onClick={() =>
                      setEditTargetEnvironments(environments.map((e) => e.slug))
                    }
                  >
                    <div
                      className={cn(
                        "w-4 h-4 rounded-sm border flex items-center justify-center",
                        editTargetEnvironments.length === environments.length
                          ? "bg-primary border-primary text-primary-foreground"
                          : "border-border",
                      )}
                    >
                      {editTargetEnvironments.length === environments.length && (
                        <Check className="h-3 w-3" />
                      )}
                    </div>
                    All Environments
                  </div>
                  {environments.map((env) => (
                    <div
                      key={env.slug}
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => {
                        if (editTargetEnvironments.includes(env.slug)) {
                          setEditTargetEnvironments(
                            editTargetEnvironments.filter((s) => s !== env.slug),
                          );
                        } else {
                          setEditTargetEnvironments([
                            ...editTargetEnvironments,
                            env.slug,
                          ]);
                        }
                      }}
                    >
                      <div
                        className={cn(
                          "w-4 h-4 rounded-sm border flex items-center justify-center",
                          editTargetEnvironments.includes(env.slug)
                            ? "bg-primary border-primary text-primary-foreground"
                            : "border-border",
                        )}
                      >
                        {editTargetEnvironments.includes(env.slug) && (
                          <Check className="h-3 w-3" />
                        )}
                      </div>
                      {env.name}
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
            </div>

            {editingVar?.isUserProvided && (
              <div className="bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-900 rounded-md p-3">
                <p className="text-xs text-yellow-800 dark:text-yellow-200">
                  This is a sensitive variable. Values are provided at runtime.
                </p>
              </div>
            )}
          </div>

          <SheetFooter className="px-6 py-4 border-t flex-row justify-end items-center gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                setEditingVar(null);
                setEditValue("");
                setEditTargetEnvironments([]);
                setEditValueVisible(false);
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={() => {
                if (!editingVar || (!editValue && !editingVar.isUserProvided))
                  return;

                // Save to selected environments
                if (
                  !editingVar.isUserProvided &&
                  editValue &&
                  editTargetEnvironments.length > 0
                ) {
                  editTargetEnvironments.forEach((envSlug) => {
                    updateEnvironmentMutation.mutate({
                      request: {
                        slug: envSlug,
                        updateEnvironmentRequestBody: {
                          entriesToUpdate: [
                            { name: editingVar.key, value: editValue },
                          ],
                          entriesToRemove: [],
                        },
                      },
                    });
                  });

                  // Update the local state
                  setEnvVars(
                    envVars.map((v) =>
                      v.id === editingVar.id
                        ? {
                            ...v,
                            value: editValue,
                            targetEnvironments: editTargetEnvironments,
                            updatedAt: new Date(),
                          }
                        : v,
                    ),
                  );

                  toast.success(`Updated ${editingVar.key}`);

                  telemetry.capture("environment_event", {
                    action: "environment_variable_updated",
                    toolset_slug: toolset.slug,
                  });
                }

                // Close the sheet
                setEditingVar(null);
                setEditValue("");
                setEditTargetEnvironments([]);
                setEditValueVisible(false);
              }}
              disabled={
                !editValue && !editingVar?.isUserProvided ||
                editTargetEnvironments.length === 0
              }
            >
              Save
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

    </div>
  );
}