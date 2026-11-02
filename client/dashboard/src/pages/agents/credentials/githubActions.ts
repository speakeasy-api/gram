import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { hooksIngestPolicyGrant } from "../agent-hook-events";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { SetupServer } from "./setupInstructions";
import { SUBJECT_CHOICE } from "./workloadCatalog";

/**
 * GitHub Actions' guided setup, as data: the grants a job's device agent
 * needs, the checks on the repository it names, the plugins it will receive,
 * and the workflow it runs.
 */

const ACTION = "speakeasy-api/device-agent-action";
export const ACTION_DOCS = `https://github.com/${ACTION}`;

export const OIDC_DOCS =
  "https://docs.github.com/en/actions/reference/security/oidc";

/** The catalog key of the GitHub Actions platform. */
export const GITHUB_ACTIONS_KEY = "github-actions";

/**
 * Every action the generated workflow uses, pinned to a commit rather than a
 * branch or tag: the job grants id-token: write, so each step can mint the
 * job's OIDC token, and a moved ref would run code nobody reviewed with the
 * agent's access. The tag or branch each commit was taken from is kept
 * beside it, so bumping one is a matter of looking that ref up again.
 *
 * speakeasyd is the exception: the device agent action installs the latest
 * stable release, checked against Speakeasy's release records, unless the
 * step sets `version`.
 */
const PINNED_ACTIONS = {
  checkout: {
    uses: "actions/checkout",
    commit: "3d3c42e5aac5ba805825da76410c181273ba90b1",
    ref: "v7",
  },
  deviceAgent: {
    uses: ACTION,
    commit: "596e9f8598bf2896e6dda3c45eb098313b671962",
    ref: "main",
  },
  claude: {
    uses: "anthropics/claude-code-action",
    commit: "6d5ae526d25ce88daa2200be073e1a373167f064",
    ref: "v1",
  },
  codex: {
    uses: "openai/codex-action",
    commit: "bdf19a4a223ec2549a3e2274a0cf61556bc07675",
    ref: "v1",
  },
} as const;

/** A step's `uses:` value for a pinned action, naming the ref it came from. */
function uses(action: keyof typeof PINNED_ACTIONS): string {
  const pinned = PINNED_ACTIONS[action];
  return `${pinned.uses}@${pinned.commit} # ${pinned.ref}`;
}

/**
 * The coding agents' CLIs, pinned to a version for the same reason as the
 * actions: they are installed in a job that can mint its OIDC token.
 */
const PINNED_PACKAGES = {
  claude: "@anthropic-ai/claude-code@2.1.314",
  codex: "@openai/codex@0.166.0",
} as const;

/** What the workflow needs to run each coding agent after Speakeasy syncs. */
export const WORKFLOW_AGENTS = {
  claude: {
    name: "Claude Code",
    secret: "ANTHROPIC_API_KEY",
    docs: "https://github.com/anthropics/claude-code-action",
    install: [
      `          npm install -g ${PINNED_PACKAGES.claude}`,
      '          mkdir -p "$HOME/.claude"',
    ],
    inputs: [
      "          anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}",
      "          github_token: ${{ github.token }}",
    ],
  },
  codex: {
    name: "Codex",
    secret: "OPENAI_API_KEY",
    docs: "https://learn.chatgpt.com/docs/github-action",
    install: [
      `          npm install -g ${PINNED_PACKAGES.codex}`,
      // runner.temp is not available in a job-level env, so later steps get
      // CODEX_HOME through GITHUB_ENV.
      '          echo "CODEX_HOME=$RUNNER_TEMP/speakeasy-codex" >> "$GITHUB_ENV"',
      '          mkdir -p "$RUNNER_TEMP/speakeasy-codex"',
    ],
    inputs: [
      "          openai-api-key: ${{ secrets.OPENAI_API_KEY }}",
      "          codex-home: ${{ env.CODEX_HOME }}",
    ],
  },
} as const;

export type WorkflowAgent = keyof typeof WORKFLOW_AGENTS;

/** The coding agents a workflow can run, in the order they are offered. */
const WORKFLOW_AGENT_KEYS = Object.keys(WORKFLOW_AGENTS) as WorkflowAgent[];

/**
 * The workflow agents as the dashboard shows AI tools elsewhere: the product's
 * own name and logo, from the agent provider registry.
 */
export const WORKFLOW_AGENT_OPTIONS: {
  id: WorkflowAgent;
  name: string;
  iconSource: string;
}[] = WORKFLOW_AGENT_KEYS.map((id) => ({
  id,
  name: AGENT_PROVIDERS[id].name,
  iconSource: AGENT_PROVIDERS[id].iconSource,
}));

/**
 * The control plane the action talks to when its input is left out. The token
 * audience is built from it, so a deployment at any other address names its
 * own in the workflow.
 */
export const ACTION_DEFAULT_CONTROL_PLANE = "https://ai.speakeasy.com";

/** What the device agent needs from the agent, beyond its MCP servers. */
export const DEVICE_AGENT_SYNC_SCOPE = "org:device_agent_sync";

/**
 * The grants a job's device agent needs: polling for the agent's plugins
 * organization-wide, and hook events for the one project the workflow names.
 * The hook grant is the one every new agent is created with when its creator
 * may give it, so the two collapse into one; a job needs it either way, and
 * a workflow edited to name another project is refused.
 */
export function githubActionsAgentGrants(
  organizationId: string,
  projectId: string,
): AgentPolicyGrantForm[] {
  return [
    {
      effect: "allow",
      scope: DEVICE_AGENT_SYNC_SCOPE,
      selector: { resourceKind: "org", resourceId: "*" },
    },
    hooksIngestPolicyGrant(organizationId, projectId),
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

/**
 * The catalog's rules narrower than the whole repository, by their keys. Each
 * key is also the variable its template adds, which the catalog test pins.
 */
export const NARROWER_SCOPES = ["branch", "environment"] as const;

/**
 * Which of the repository's jobs the rule admits, which the workflow has to
 * match: every job, the jobs on one branch, or the jobs that use one
 * environment.
 */
export type JobScope =
  | { kind: "repository" }
  | { kind: (typeof NARROWER_SCOPES)[number]; name: string };

/** The scope the setup's values choose; its name is empty until typed. */
export function jobScope(values: Record<string, string>): JobScope {
  const kind = NARROWER_SCOPES.find(
    (scope) => scope === values[SUBJECT_CHOICE],
  );
  if (kind === undefined) return { kind: "repository" };
  return { kind, name: values[kind]?.trim() ?? "" };
}

/** The repository as GitHub names it, for prose and the workflow. */
export function repositoryName(values: Record<string, string>): string {
  const name = (key: string, fallback: string) =>
    (values[key]?.trim() || fallback).replace(IMMUTABLE_ID, "");
  return `${name("owner", "OWNER")}/${name("repo", "REPO")}`;
}

/** The control plane URL as the workflow names it, without trailing slashes. */
function trimmedControlPlane(controlPlaneUrl: string): string {
  return controlPlaneUrl.replace(/\/+$/, "");
}

/**
 * Why the action cannot talk to this control plane, or null when it can. The
 * action refuses anything but https, since the job's token exchange crosses
 * the network.
 */
export function controlPlaneProblem(controlPlaneUrl: string): string | null {
  const controlPlane = trimmedControlPlane(controlPlaneUrl);
  if (controlPlane === "" || controlPlane.startsWith("https://")) return null;
  return `This deployment serves Speakeasy at ${controlPlane}, and the action only talks to an https control plane: the job's GitHub token would cross the network unencrypted. Serve Speakeasy over https to use GitHub Actions.`;
}

/**
 * The job's workflow, or why there is none: a deployment the action cannot
 * reach (see controlPlaneProblem) gets no workflow at all. The organization ID
 * and the hook events project are filled in, and a deployment other than the
 * action's default names itself, because the token's audience is built from
 * it.
 */
export function workflowYaml({
  agent,
  organizationId,
  projectSlug,
  controlPlaneUrl,
  scope,
}: {
  agent: WorkflowAgent;
  organizationId: string;
  projectSlug: string;
  controlPlaneUrl: string;
  scope: JobScope;
}): { yaml: string; problem: null } | { yaml: null; problem: string } {
  const problem = controlPlaneProblem(controlPlaneUrl);
  if (problem !== null) return { yaml: null, problem };
  const controlPlane = trimmedControlPlane(controlPlaneUrl);
  const selected = WORKFLOW_AGENTS[agent];
  // A rule narrower than the repository refuses a pull request's job, so only
  // the whole repository's workflow runs on one. Names are written as JSON
  // strings, which YAML reads as they are; the catalog's branch pattern
  // refuses the characters a branch filter reads as a glob.
  const lines = [
    "name: Speakeasy",
    "",
    "on:",
    "  push:",
    `    branches: [${scope.kind === "branch" ? JSON.stringify(scope.name || "BRANCH") : "main"}]`,
    ...(scope.kind === "repository" ? ["  pull_request:"] : []),
    "",
    "jobs:",
    "  agent:",
    ...(scope.kind === "repository"
      ? [
          "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.fork == false",
        ]
      : []),
    "    runs-on: ubuntu-latest",
    ...(scope.kind === "environment"
      ? [
          `    environment: ${JSON.stringify(scope.name || "ENVIRONMENT")} # its protection rules pass before the job starts`,
        ]
      : []),
    "    permissions:",
    "      id-token: write # lets the job request its GitHub OIDC token",
    "      contents: read",
    "    steps:",
    `      - uses: ${uses("checkout")}`,
    "        with:",
    "          persist-credentials: false",
    "      # Install the tool and create its configuration directory before Speakeasy syncs.",
    `      - name: Install ${selected.name}`,
    "        run: |",
    ...selected.install,
    "      # Installs the latest stable speakeasyd; add version: <x.y.z> to pin one.",
    "      - name: Connect to Speakeasy",
    "        id: speakeasy",
    `        uses: ${uses("deviceAgent")}`,
    "        with:",
    `          organization-id: ${organizationId || "SPEAKEASY_ORGANIZATION_ID"}`,
    `          project: ${projectSlug || "SPEAKEASY_PROJECT"} # hook events go to this project`,
    ...(controlPlane !== "" && controlPlane !== ACTION_DEFAULT_CONTROL_PLANE
      ? [`          control-plane-url: ${controlPlane}`]
      : []),
    "      # Lists the agent's Speakeasy MCP servers; each one connects with a fresh token.",
    `      - run: ${agent} mcp list`,
    `      - name: Run ${selected.name}`,
    `        uses: ${uses(agent)}`,
    "        with:",
    ...selected.inputs,
    "          prompt: Describe this repository and list the Speakeasy MCP tools available to you. Do not modify files.",
  ];
  return { yaml: lines.join("\n"), problem: null };
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

type PluginServer = NonNullable<Plugin["servers"]>[number];

/**
 * The IDs a plugin server can be granted by. A gateway has neither: it is not
 * a server an agent is given.
 */
function grantableIds(server: PluginServer): string[] {
  return [server.toolsetId, server.mcpServerId].filter(
    (id): id is string => id !== undefined && id !== "",
  );
}

/** The plugin servers matching `keep`, by name, once each and sorted. */
function serverNames(
  plugins: Plugin[],
  keep: (server: PluginServer) => boolean,
): string[] {
  const names = new Set(
    plugins
      .flatMap((plugin) => plugin.servers ?? [])
      .filter(keep)
      .map((server) => server.displayName),
  );
  return [...names].sort();
}

/**
 * Whether a plugin server has no Speakeasy authorization server, which is a
 * server with no user session issuer. It is read from the organization's row
 * for the server the plugin names, or for the one wrapping the toolset it
 * names; a server missing from that list is not guessed at.
 */
function lacksAuthorizationServer(
  server: PluginServer,
  mcpServers: McpServer[],
): boolean {
  // A toolset is served by the row that stands in for it, not by any
  // wrapper that names it, such as a gateway member's.
  const row = mcpServers.find((candidate) =>
    server.mcpServerId
      ? candidate.id === server.mcpServerId
      : candidate.toolsetId === server.toolsetId &&
        standsInForToolset(candidate),
  );
  return row !== undefined && !row.userSessionIssuerId;
}

/**
 * The servers in these plugins with no Speakeasy authorization server.
 * Speakeasy gives a job's device agent no token endpoint for one, so the job
 * skips it, as it does a gateway.
 */
export function pluginServersWithoutAuthorization(
  plugins: Plugin[],
  mcpServers: McpServer[],
): string[] {
  return serverNames(plugins, (server) =>
    lacksAuthorizationServer(server, mcpServers),
  );
}

/**
 * The servers in these plugins the agent was not given. The job is told
 * about them and cannot get a token for them, so its tools show them as
 * needing authentication. One the job skips anyway, for having no Speakeasy
 * authorization server, is not among them: giving it to the agent would not
 * help.
 */
export function unreachablePluginServers(
  plugins: Plugin[],
  servers: SetupServer[],
  mcpServers: McpServer[],
): string[] {
  const reachable = new Set(servers.flatMap((s) => [s.id, s.resourceId]));
  return serverNames(plugins, (server) => {
    const ids = grantableIds(server);
    return (
      ids.length > 0 &&
      !ids.some((id) => reachable.has(id)) &&
      !lacksAuthorizationServer(server, mcpServers)
    );
  });
}

/**
 * The gateways in these plugins. Gateways do not accept agents yet, so a job
 * skips them: they are listed but give it no tools.
 */
export function pluginGateways(plugins: Plugin[]): string[] {
  return serverNames(plugins, (server) => grantableIds(server).length === 0);
}
