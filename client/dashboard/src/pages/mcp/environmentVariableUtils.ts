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

// Check if an environment has all required variables configured
export const environmentHasAllRequiredVariables = (
  environmentSlug: string,
  requiredVars: EnvironmentVariable[],
): boolean => {
  return requiredVars.every((v) => environmentHasValue(v, environmentSlug));
};

// Returns keys of variables that will be server-injected for every caller of the
// MCP server — i.e. variables in `state: "system"` with a value present in the
// attached environment. Used to warn users before flipping an MCP to public.
export const getSystemProvidedVariables = (
  envVars: EnvironmentVariable[],
  attachedEnvironmentSlug: string,
): string[] =>
  envVars
    .filter((v) => v.state === "system")
    .filter((v) => environmentHasValue(v, attachedEnvironmentSlug))
    .map((v) => v.key);
