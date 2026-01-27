export type EnvVarState = "user-provided" | "system" | "omitted";

export interface EnvironmentVariable {
  id: string;
  key: string;
  // Track multiple values per variable - each value can be in different environments
  valueGroups: Array<{
    valueHash: string;
    value: string; // Redacted value for display
    environments: string[]; // Environment slugs that have this value
  }>;
  state: EnvVarState;
  isRequired: boolean; // True for advertised vars from toolset, false for custom user-added
  description?: string; // Optional description for required vars
  createdAt?: Date;
  updatedAt?: Date;
}

// Get all environments that have a value for a specific variable
export const getAllEnvironments = (envVar: EnvironmentVariable): string[] => {
  const allEnvs = new Set<string>();
  envVar.valueGroups.forEach((group) => {
    group.environments.forEach((env) => allEnvs.add(env));
  });
  return Array.from(allEnvs);
};

// Check if an environment has a value for a specific variable
export const environmentHasValue = (
  envVar: EnvironmentVariable,
  environmentSlug: string,
): boolean => {
  if (envVar.state === "user-provided" || envVar.state === "omitted")
    return true;
  return envVar.valueGroups.some((group) =>
    group.environments.includes(environmentSlug),
  );
};

// Get the value for a variable in a specific environment
export const getValueForEnvironment = (
  envVar: EnvironmentVariable,
  environmentSlug: string,
): string => {
  const group = envVar.valueGroups.find((g) =>
    g.environments.includes(environmentSlug),
  );
  return group?.value || "";
};

// Check if a variable has a header display name override
export const hasHeaderOverride = (
  envVar: EnvironmentVariable,
  environmentEntries: Array<{
    variableName: string;
    headerDisplayName?: string;
  }>,
): boolean => {
  const entry = environmentEntries.find((e) => e.variableName === envVar.key);
  return !!entry?.headerDisplayName;
};

// Get header display name for a variable
export const getHeaderDisplayName = (
  envVar: EnvironmentVariable,
  environmentEntries: Array<{
    variableName: string;
    headerDisplayName?: string;
  }>,
  editingState: Map<string, { value: string; headerDisplayName?: string }>,
): string => {
  if (
    editingState.has(envVar.id) &&
    editingState.get(envVar.id)!.headerDisplayName !== undefined
  ) {
    return editingState.get(envVar.id)!.headerDisplayName!;
  }
  const entry = environmentEntries.find((e) => e.variableName === envVar.key);
  return entry?.headerDisplayName || "";
};

// Get editing value for a variable (either from editing state or from valueGroups)
export const getEditingValue = (
  envVar: EnvironmentVariable,
  editingState: Map<string, { value: string; headerDisplayName?: string }>,
  selectedEnvironmentView: string,
): string => {
  if (editingState.has(envVar.id)) {
    return editingState.get(envVar.id)!.value;
  }
  // Show the value for the currently selected environment
  return getValueForEnvironment(envVar, selectedEnvironmentView);
};

// Check if a variable has unsaved changes or has no environment entry (unmapped)
export const hasUnsavedChanges = (
  envVar: EnvironmentVariable,
  environmentEntries: Array<{
    variableName: string;
    providedBy: string;
    headerDisplayName?: string;
  }>,
  editingState: Map<string, { value: string; headerDisplayName?: string }>,
  selectedEnvironmentView: string,
): boolean => {
  // Find existing environment entry
  const entry = environmentEntries.find((e) => e.variableName === envVar.key);

  // If no entry exists, this is an unmapped required variable that needs to be saved
  if (!entry && envVar.isRequired) {
    return true;
  }

  // Determine the original state based on environment entry
  const originalState: EnvVarState =
    entry?.providedBy === "user"
      ? "user-provided"
      : entry?.providedBy === "none"
        ? "omitted"
        : "system";

  // Check if state has changed
  if (envVar.state !== originalState) {
    return true;
  }

  // If state is system, check if value changed
  if (envVar.state === "system" && editingState.has(envVar.id)) {
    const editing = editingState.get(envVar.id)!;
    const currentValue = getValueForEnvironment(
      envVar,
      selectedEnvironmentView,
    );

    // Check if value changed (only if a value is provided)
    if (editing.value && editing.value !== currentValue) {
      return true;
    }
  }

  // Check if header display name changed
  if (editingState.has(envVar.id)) {
    const editing = editingState.get(envVar.id)!;
    const originalHeaderName = entry?.headerDisplayName || "";
    if (
      editing.headerDisplayName !== undefined &&
      editing.headerDisplayName !== originalHeaderName
    ) {
      return true;
    }
  }

  return false;
};

// Check if an environment has all required variables configured
export const environmentHasAllRequiredVariables = (
  environmentSlug: string,
  requiredVars: EnvironmentVariable[],
): boolean => {
  return requiredVars.every((v) => environmentHasValue(v, environmentSlug));
};
