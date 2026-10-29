import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { SetupServer } from "./setupInstructions";

/**
 * GitHub Actions' guided setup, as data: the grants a job's device agent
 * needs, the checks on the repository it names, the plugins it will receive,
 * and the workflow it runs.
 */

const ACTION = "speakeasy-api/device-agent-action";
export const ACTION_DOCS = `https://github.com/${ACTION}`;
export const OIDC_DOCS =
  "https://docs.github.com/en/actions/reference/security/oidc";

/**
 * The control plane the action talks to when its input is left out. The token
 * audience is built from it, so a deployment at any other address names its
 * own in the workflow.
 */
export const ACTION_DEFAULT_CONTROL_PLANE = "https://ai.speakeasy.com";

/**
 * The device-agent branch with GitHub Actions support. No speakeasyd release
 * carries it yet, so the workflow builds speakeasyd from it until one does.
 */
const SPEAKEASYD_SOURCE_REF =
  "brad/dno-1341-da-mint-and-rotate-the-mcpconnect-credential-from-the";

/** What the device agent needs from the agent, beyond its MCP servers. */
export const DEVICE_AGENT_SYNC_SCOPE = "org:device_agent_sync";
export const HOOKS_INGEST_SCOPE = "org:hooks_ingest";

/**
 * The grants a job's device agent needs: polling for the agent's plugins
 * organization-wide, and hook events for the one project the workflow names.
 * Narrower than the agent permission editor's organization-wide hooks grant,
 * so a workflow edited to name another project is refused there.
 */
export function githubActionsAgentGrants(
  projectId: string,
): AgentPolicyGrantForm[] {
  return [
    {
      effect: "allow",
      scope: DEVICE_AGENT_SYNC_SCOPE,
      selector: { resourceKind: "org", resourceId: "*" },
    },
    {
      effect: "allow",
      scope: HOOKS_INGEST_SCOPE,
      selector: { resourceKind: "org", resourceId: "*", projectId },
    },
  ];
}

/** Matches the `@<id>` an immutable subject adds to a name. */
const IMMUTABLE_ID = /@[0-9]+$/;

/**
 * Why the owner and repository cannot make one subject, or null when they
 * can. GitHub puts IDs on both or on neither, so a rule with one ID would
 * never match a job.
 */
export function repositoryProblem(
  values: Record<string, string>,
): string | null {
  const owner = values["owner"]?.trim() ?? "";
  const repo = values["repo"]?.trim() ?? "";
  if (owner === "" || repo === "") return null;
  if (IMMUTABLE_ID.test(owner) !== IMMUTABLE_ID.test(repo)) {
    return "GitHub adds IDs to both the owner and the repository, or to neither. Add an @ID to both, or remove it from both.";
  }
  return null;
}

/** The repository as GitHub names it, for prose and the workflow. */
export function repositoryName(values: Record<string, string>): string {
  const name = (key: string, fallback: string) =>
    (values[key]?.trim() || fallback).replace(IMMUTABLE_ID, "");
  return `${name("owner", "OWNER")}/${name("repo", "REPO")}`;
}

/**
 * The job's workflow. The organization ID and the hook events project are
 * filled in, and a deployment other than the action's default names itself,
 * because the token's audience is built from it.
 */
export function workflowYaml({
  organizationId,
  projectSlug,
  controlPlaneUrl,
  buildFromSource,
}: {
  organizationId: string;
  projectSlug: string;
  controlPlaneUrl: string;
  /** Builds speakeasyd from source, until a release supports GitHub Actions. */
  buildFromSource: boolean;
}): string {
  const controlPlane = controlPlaneUrl.replace(/\/+$/, "");
  const lines = [
    "name: Speakeasy",
    "",
    "on:",
    "  push:",
    "    branches: [main]",
    "  pull_request:",
    "",
    "jobs:",
    "  agent:",
    "    runs-on: ubuntu-latest",
    "    permissions:",
    "      id-token: write # lets the job request its GitHub OIDC token",
    "      contents: read",
    "    steps:",
    "      # The action writes each installed tool's MCP servers, so install the tools first.",
    "      - run: npm install -g @anthropic-ai/claude-code",
  ];
  if (buildFromSource) {
    lines.push(
      "      # Until a speakeasyd release supports GitHub Actions, build it from source.",
      "      - uses: actions/checkout@v7",
      "        with:",
      "          repository: speakeasy-api/device-agent",
      `          ref: ${SPEAKEASYD_SOURCE_REF}`,
      "          path: .speakeasyd-src",
      "          persist-credentials: false",
      "      - uses: actions/setup-go@v7",
      "        with:",
      "          go-version-file: .speakeasyd-src/go.mod",
      '      - run: CGO_ENABLED=0 go build -o "$RUNNER_TEMP/speakeasyd" ./cmd/speakeasyd',
      "        working-directory: .speakeasyd-src",
    );
  }
  lines.push(
    `      - uses: ${ACTION}@main`,
    "        with:",
    `          organization-id: ${organizationId || "SPEAKEASY_ORGANIZATION_ID"}`,
    `          project: ${projectSlug || "SPEAKEASY_PROJECT"} # hook events go to this project`,
  );
  if (controlPlane !== "" && controlPlane !== ACTION_DEFAULT_CONTROL_PLANE) {
    lines.push(`          control-plane-url: ${controlPlane}`);
  }
  if (buildFromSource) {
    lines.push("          speakeasyd-binary: ${{ runner.temp }}/speakeasyd");
  }
  lines.push(
    "      # Lists the agent's Speakeasy MCP servers; each one connects with a fresh token.",
    "      - run: claude mcp list",
  );
  return lines.join("\n");
}

/**
 * The plugins a new agent receives: those assigned to everyone in this
 * project. An agent is never a plugin assignment's audience itself, only
 * through a role it holds, and `available` plugins are never installed on an
 * agent's machine because nobody there can turn them on.
 */
export function pluginsForEveryAgent(plugins: Plugin[]): Plugin[] {
  return plugins.filter((plugin) =>
    (plugin.assignments ?? []).some(
      (assignment) =>
        assignment.principalUrn === "*" &&
        assignment.installMode !== "available",
    ),
  );
}

/**
 * The servers in these plugins the agent was not given. The job is told
 * about them and cannot get a token for them, so its tools show them as
 * needing authentication.
 */
export function unreachablePluginServers(
  plugins: Plugin[],
  servers: SetupServer[],
): string[] {
  const reachable = new Set(servers.flatMap((s) => [s.id, s.resourceId]));
  const names = new Set<string>();
  for (const plugin of plugins) {
    for (const server of plugin.servers ?? []) {
      const ids = [server.toolsetId, server.mcpServerId].filter(
        (id): id is string => id !== undefined && id !== "",
      );
      // Gateways do not accept agents yet, so a job skips them either way.
      if (ids.length === 0) continue;
      if (!ids.some((id) => reachable.has(id))) names.add(server.displayName);
    }
  }
  return [...names].sort();
}
