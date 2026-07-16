export type EnvVarState = "user-provided" | "system" | "omitted";

export interface EnvironmentVariable {
  id: string;
  key: string;
  // The value this variable holds in each environment that defines it. A secret
  // entry only ever arrives redacted (e.g. "sup*****"); a non-secret one
  // arrives in cleartext and is safe to show as-is.
  environmentValues: Array<{
    environmentSlug: string;
    value: string;
    isSecret: boolean;
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

// Whether a specific environment stores this variable at all. Distinct from
// environmentHasValue, which reports true for states that never store one.
export const hasEntryInEnvironment = (
  envVar: EnvironmentVariable,
  environmentSlug: string,
): boolean =>
  envVar.environmentValues.some((v) => v.environmentSlug === environmentSlug);

// Whether a variable is stored as a secret in a specific environment. A
// variable this environment does not define yet is treated as secret, matching
// the server's default for new entries.
export const isSecretInEnvironment = (
  envVar: EnvironmentVariable,
  environmentSlug: string,
): boolean => {
  const entry = envVar.environmentValues.find(
    (v) => v.environmentSlug === environmentSlug,
  );
  return entry ? entry.isSecret : true;
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
  editingState: Map<string, { headerDisplayName?: string }>,
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
