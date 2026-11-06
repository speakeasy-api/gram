import { salesforceSelectionForUrl } from "@/pages/catalog/provider-setup/salesforce";
import {
  initialState,
  steps,
  type State,
  type Provider,
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
    if (url.origin === "https://api.githubcopilot.com" && path === "/mcp")
      return "github";
    if (salesforceSelectionForUrl(value)) return "salesforce";
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
export function saveProviderProgress(key: string, state: State): void {
  try {
    sessionStorage.setItem(
      key,
      JSON.stringify({
        revision: 2,
        step: state.step,
        done: state.done.filter(
          (step) => !["credentials", "test"].includes(step),
        ),
        values: {
          ...(state.provider === "github" ? { org: state.values.org } : {}),
          ...(state.provider === "salesforce"
            ? {
                integration: state.values.integration,
                environment: state.values.environment,
                salesforceServer: state.values.salesforceServer,
                instanceUrl: state.values.instanceUrl,
                path: state.values.path,
                configurationPending: state.configChanged,
                sourceFingerprint: state.sourceFingerprint,
              }
            : {}),
          appChoice: state.values.appChoice,
          ...(state.accessChosen ? { access: state.values.access } : {}),
        },
      }),
    );
  } catch {
    /* Storage may be disabled. The flow still works in memory. */
  }
}
export function loadProviderProgress(
  provider: SetupProvider,
  key: string,
  defaults?: Partial<State["values"]>,
  sourceFingerprint?: string,
): State {
  const initial = initialState(provider);
  Object.assign(initial.values, defaults);
  if (provider === "salesforce") initial.sourceFingerprint = sourceFingerprint;
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (
      !value ||
      typeof value !== "object" ||
      !("revision" in value) ||
      (value.revision !== 1 && value.revision !== 2)
    )
      return initial;
    if (
      !("values" in value) ||
      !value.values ||
      typeof value.values !== "object"
    )
      return initial;
    const { values } = value;
    const changedSource =
      provider === "salesforce" &&
      !!defaults &&
      !!sourceFingerprint &&
      (!("sourceFingerprint" in values) ||
        values.sourceFingerprint !== sourceFingerprint);
    if (
      provider === "github" &&
      "org" in values &&
      typeof values.org === "string" &&
      values.org.length <= 10000
    )
      initial.values.org = values.org;
    if (
      "appChoice" in values &&
      (values.appChoice === "new" || values.appChoice === "existing")
    )
      initial.values.appChoice = values.appChoice;
    if (
      "access" in values &&
      (values.access === "all" || values.access === "read") &&
      // Legacy snapshots always wrote the default; retain only the narrower choice.
      (value.revision === 2 || values.access === "read")
    ) {
      initial.values.access = values.access;
      initial.accessChosen = true;
    }
    if (provider === "salesforce") {
      initial.configChanged =
        "configurationPending" in values &&
        values.configurationPending === true;
      for (const field of [
        "integration",
        "environment",
        "salesforceServer",
        "instanceUrl",
      ] as const) {
        const candidate = (values as Record<string, unknown>)[field];
        if (
          (!defaults ||
            !sourceFingerprint ||
            (initial.configChanged && !changedSource)) &&
          typeof candidate === "string" &&
          candidate.length <= 2048
        )
          initial.values[field] = candidate;
      }
      if (
        "path" in values &&
        (values.path === "own" || values.path === "support")
      )
        initial.values.path = values.path;
      if (!["native", "hosted"].includes(initial.values.integration))
        initial.values.integration = "native";
      if (!["production", "sandbox"].includes(initial.values.environment))
        initial.values.environment = "production";
      if (!["own", "support"].includes(initial.values.path))
        initial.values.path = "own";
    }
    // Resolve branch-specific steps after restoring choices.
    const validSteps = steps(initial).map((step) => step.id);
    if (
      !changedSource &&
      "step" in value &&
      typeof value.step === "string" &&
      validSteps.includes(value.step)
    )
      initial.step = value.step;
    if (changedSource) initial.step = "choices";
    if (!changedSource && "done" in value && Array.isArray(value.done))
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

export function salesforceSourceFingerprint(
  remoteMcpServerId: string | undefined,
  source: { url: string; instanceUrl?: string },
): string {
  return JSON.stringify([
    remoteMcpServerId,
    source.url,
    source.instanceUrl ?? "",
  ]);
}
