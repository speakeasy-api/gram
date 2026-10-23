import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import type { Environment } from "@gram/client/models/components/environment.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";

/** Project creation provisions this environment; an unset default inherits it. */
const PROJECT_DEFAULT_ENVIRONMENT_SLUG = "default";

type BuiltinCredential = {
  /** The environment variable the value is stored under. */
  variable: string;
  /** "SLACK_BOT_TOKEN" reads as "Slack bot token". */
  label: string;
  set: boolean;
};

export type BuiltinMcpCredentials = {
  environments: Environment[];
  /** The environment the server reads credentials from today. */
  environment: Environment | undefined;
  credentials: BuiltinCredential[];
  /** False while nothing the server can authenticate with is stored. */
  ready: boolean;
  isLoading: boolean;
  isError: boolean;
};

/**
 * What this server can authenticate upstream with right now. The built-in
 * reads its credentials from a project environment exactly as any other server
 * does, so "ready" is simply whether one of the values it accepts is stored
 * where it will look.
 */
export function useBuiltinMcpCredentials(
  server: BuiltinMcpServer | undefined,
  /**
   * The environment to read against instead of the saved one: `null` is the
   * project default. Lets a form preview an unsaved selection.
   */
  pendingEnvironmentId?: string | null,
): BuiltinMcpCredentials {
  const gramProject = useProjectSlugForRequests();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  // Environments carry credential values, and only an admin may configure a
  // built-in, so nobody else asks for them.
  const enabled = hasScope("org:admin", organization.id);
  const { data, isLoading, isError } = useListEnvironments(
    { gramProject },
    undefined,
    { throwOnError: false, enabled },
  );
  const environments = data?.environments ?? [];
  const selectedId =
    pendingEnvironmentId === undefined
      ? server?.environmentId
      : (pendingEnvironmentId ?? undefined);
  const environment = selectedId
    ? environments.find((candidate) => candidate.id === selectedId)
    : environments.find(
        (candidate) => candidate.slug === PROJECT_DEFAULT_ENVIRONMENT_SLUG,
      );
  // Environment entry names are matched case-insensitively by the runtime.
  const stored = new Set(
    (environment?.entries ?? []).map((entry) => entry.name.toUpperCase()),
  );
  const credentials = (server?.credentialVariables ?? []).map((variable) => ({
    variable,
    label: credentialLabel(variable),
    set: stored.has(variable.toUpperCase()),
  }));

  return {
    environments,
    environment,
    credentials,
    ready: credentials.some((credential) => credential.set),
    // A disabled query never resolves, so report it as still loading rather
    // than as an answered "nothing is stored".
    isLoading: enabled ? isLoading : true,
    isError,
  };
}

/** "SLACK_BOT_TOKEN" → "Slack bot token". */
function credentialLabel(variable: string): string {
  const words = variable
    .toLowerCase()
    .split(/[_\s]+/)
    .filter(Boolean);
  const [first, ...rest] = words;
  if (!first) return variable;
  return [first.charAt(0).toUpperCase() + first.slice(1), ...rest].join(" ");
}
