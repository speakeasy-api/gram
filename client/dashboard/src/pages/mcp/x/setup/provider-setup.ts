import {
  initialState,
  steps,
  type State,
  type Provider,
  type FieldName,
} from "@/pages/catalog/provider-setup/state";
import type { ServerInstallStatus } from "@/pages/catalog/useRemoteMcpInstallWorkflow";

export type SetupProvider = Provider;

/** Match exact official upstream endpoints, never names or hostname substrings. */
export function setupProviderForUrl(
  value: string | undefined,
): SetupProvider | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    if (
      url.protocol !== "https:" ||
      url.port ||
      url.search ||
      url.hash ||
      url.username ||
      url.password
    )
      return null;
    const path = url.pathname.replace(/\/+$/, "");
    if (url.origin === "https://mcp.slack.com" && path === "/mcp")
      return "slack";
    return null;
  } catch {
    return null;
  }
}
export function providerSetupInstallTarget(
  servers: { remotes?: { url: string }[] | undefined }[],
  statuses: ServerInstallStatus[],
): string | null {
  if (
    servers.length !== 1 ||
    statuses.length !== 1 ||
    !setupProviderForUrl(statuses[0]?.remoteUrl)
  )
    return null;
  const result = statuses[0];
  if (result?.identityMode !== "user") return null;
  if (
    !result?.mcpServerId ||
    !result.mcpServerParam ||
    (result.status !== "completed" && !result.setupRequired)
  )
    return null;
  return result.mcpServerParam;
}
export function providerProgressKey(
  provider: SetupProvider,
  userId: string,
  orgId: string,
  projectId: string,
  serverId: string,
): string {
  return `provider-setup:${provider}:v1:${[userId, orgId, projectId, serverId].map(encodeURIComponent).join(":")}`;
}
const progressFields = [
  "appChoice",
  "access",
] as const satisfies readonly FieldName[];
const choiceValues: Partial<Record<FieldName, readonly string[]>> = {
  appChoice: ["new", "existing"],
  access: ["all", "read"],
};
export function saveProviderProgress(key: string, state: State): void {
  try {
    sessionStorage.setItem(
      key,
      JSON.stringify({
        revision: 1,
        step: state.step,
        done: state.done.filter(
          (step) => !["credentials", "test"].includes(step),
        ),
        complete: false,
        appChoice: state.values.appChoice,
        access: state.values.access,
        values: Object.fromEntries(
          progressFields.map((field) => [field, state.values[field]]),
        ),
      }),
    );
  } catch {
    /* Storage may be disabled. The flow still works in memory. */
  }
}
export function loadProviderProgress(
  provider: SetupProvider,
  key: string,
): State {
  const initial = initialState(provider);
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (
      !value ||
      typeof value !== "object" ||
      !("revision" in value) ||
      value.revision !== 1
    )
      return initial;
    // Accept the original flat choice snapshots as well as the values object.
    const values =
      "values" in value && value.values && typeof value.values === "object"
        ? value.values
        : value;
    for (const field of progressFields) {
      if (!(field in values)) continue;
      const entry: unknown = Reflect.get(values, field);
      const choices = choiceValues[field];
      if (
        typeof entry === "string" &&
        entry.length <= 10000 &&
        (!choices || choices.includes(entry))
      )
        initial.values[field] = entry;
    }
    // Resolve branch-specific steps after restoring choices.
    const validSteps = steps(initial).map((step) => step.id);
    if (
      "step" in value &&
      typeof value.step === "string" &&
      validSteps.includes(value.step)
    )
      initial.step = value.step;
    if ("done" in value && Array.isArray(value.done))
      initial.done = value.done.filter(
        (id: unknown): id is string =>
          typeof id === "string" &&
          validSteps.includes(id) &&
          !["credentials", "test"].includes(id),
      );
    return initial;
  } catch {
    return initial;
  }
}
