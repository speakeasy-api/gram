export type EnvVarState = "user-provided" | "system" | "omitted";

export interface EnvironmentVariable {
  id: string;
  key: string;
  // The (redacted) value this variable holds in each environment that defines it.
  environmentValues: Array<{
    environmentSlug: string;
    value: string; // Redacted value for display
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
  return envVar.environmentValues.some(
    (v) => v.environmentSlug === environmentSlug,
  );
};

// Get the value for a variable in a specific environment
export const getValueForEnvironment = (
  envVar: EnvironmentVariable,
  environmentSlug: string,
): string => {
  const entry = envVar.environmentValues.find(
    (v) => v.environmentSlug === environmentSlug,
  );
  return entry?.value || "";
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

// Get editing value for a variable (either from editing state or from environmentValues)
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

// Decide what to persist for a system-state variable on save. Loaded values
// are server-redacted (e.g. "sup*****"), so writing one back would replace the
// stored secret with the mask. Only a value the user actually typed (one that
// differs from the loaded redacted value) is an update; clearing a previously
// set value is a removal; anything else must not touch the stored value.
export type SystemValueSaveOp =
  | { kind: "update"; value: string }
  | { kind: "remove" }
  | { kind: "skip" };

export const getSystemValueSaveOp = (
  editingValue: string | undefined,
  loadedValue: string,
): SystemValueSaveOp => {
  if (editingValue === undefined) return { kind: "skip" };
  if (!editingValue) {
    return loadedValue ? { kind: "remove" } : { kind: "skip" };
  }
  if (editingValue === loadedValue) return { kind: "skip" };
  return { kind: "update", value: editingValue };
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
