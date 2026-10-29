import { Alert } from "@/components/ui/Alert";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useQueries } from "@tanstack/react-query";
import type { JSX } from "react";
import { Link } from "react-router";
import { CodeBlock } from "./CodeBlock";
import { Links, VariableField } from "./SetupFields";
import type {
  Connection,
  PlatformSetup,
  SetupServer,
} from "./setupInstructions";
import {
  subjectRule,
  type CatalogEntry,
  type CatalogVariable,
} from "./workloadCatalog";

const ACTION = "speakeasy-api/device-agent-action";
const ACTION_DOCS = `https://github.com/${ACTION}`;
const OIDC_DOCS = "https://docs.github.com/en/actions/reference/security/oidc";

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
function repositoryName(values: Record<string, string>): string {
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

function PluginsStep({
  agentName,
  servers,
}: {
  agentName: string;
  servers: SetupServer[];
}): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const sdk = useSdkClient();
  const plugins = usePlugins({ gramProject: project.slug }, undefined, {
    enabled: project.slug !== "",
    throwOnError: false,
  });
  // The list carries a count of each plugin's assignments, not the
  // assignments, so each assigned plugin is read for its audiences.
  const assigned = (plugins.data?.plugins ?? []).filter(
    (plugin) => (plugin.assignmentCount ?? 0) > 0,
  );
  const details = useQueries({
    queries: assigned.map((plugin) => ({
      queryKey: ["github-actions-setup-plugin", project.slug, plugin.id],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.plugins.getPlugin(
          { id: plugin.id, gramProject: project.slug },
          undefined,
          { signal },
        ),
      retry: false,
      throwOnError: false,
      staleTime: 30_000,
    })),
  });
  const pending = plugins.isPending || details.some((query) => query.isPending);
  const failed = plugins.isError || details.some((query) => query.isError);
  const delivered = pluginsForEveryAgent(
    details.flatMap((query) => (query.data === undefined ? [] : [query.data])),
  );
  const unreachable = unreachablePluginServers(delivered, servers);

  return (
    <div className="space-y-4 text-sm">
      <p>
        The job gets {agentName || "this agent"}&apos;s plugins: their MCP
        servers, skills and hooks. Hook events need the observability plugin
        among them.
      </p>
      {pending ? (
        <p className="text-muted-foreground">
          Reading this project&apos;s plugins…
        </p>
      ) : failed ? (
        <Alert variant="warning">
          <div className="text-sm">
            This project&apos;s plugins could not be read. Check them on the{" "}
            <Link className="underline" to={routes.plugins.href()}>
              Plugins page
            </Link>
            .
          </div>
        </Alert>
      ) : delivered.length === 0 ? (
        <Alert variant="warning">
          <div className="text-sm">
            No plugin in this project reaches {agentName || "this agent"}, so
            the job gets no MCP servers and sends no hook events. On the{" "}
            <Link className="underline" to={routes.plugins.href()}>
              Plugins page
            </Link>
            , assign a plugin to <strong>Everyone</strong>, or to a role and
            give the agent that role.
          </div>
        </Alert>
      ) : (
        <ul className="border-border divide-border divide-y border">
          {delivered.map((plugin) => (
            <li key={plugin.id} className="px-3 py-2.5">
              <p className="font-medium">{plugin.name}</p>
              <p className="text-muted-foreground">
                {(plugin.servers ?? []).length === 0
                  ? "No MCP servers"
                  : (plugin.servers ?? [])
                      .map((server) => server.displayName)
                      .join(", ")}
              </p>
            </li>
          ))}
        </ul>
      )}
      {unreachable.length > 0 && (
        <Alert variant="warning">
          <div className="text-sm">
            {agentName || "This agent"} cannot use {unreachable.join(", ")}, so
            the job&apos;s tools will show{" "}
            {unreachable.length === 1 ? "it" : "them"} as needing
            authentication. Go back to MCP Selection to add{" "}
            {unreachable.length === 1 ? "it" : "them"}.
          </div>
        </Alert>
      )}
      <p className="text-muted-foreground">
        Plugins assigned to a role reach the agent too, once it holds that role.
        Servers that need each person to sign in do not work in a job: there is
        nobody to sign in.
      </p>
    </div>
  );
}

function WorkflowStep(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const yaml = workflowYaml({
    organizationId: organization.id,
    projectSlug: project.slug,
    controlPlaneUrl: getServerURL(),
    buildFromSource: true,
  });
  return (
    <div className="space-y-4 text-sm">
      <p>
        Add this workflow to the repository, or add the steps to a job you
        already have. The job proves who it is with its own GitHub token, so no
        Speakeasy secret is stored in GitHub.
      </p>
      <CodeBlock
        value={yaml}
        label=".github/workflows/speakeasy.yml"
        language="yaml"
      />
      <Alert variant="info">
        <div className="space-y-2 text-sm">
          <p>
            <strong>Preview.</strong> The action needs a speakeasyd with GitHub
            Actions support, which no release includes yet, so this workflow
            builds speakeasyd from source. Remove those three steps and the{" "}
            <code>speakeasyd-binary</code> input once a release supports it.
          </p>
        </div>
      </Alert>
      <ul className="list-disc space-y-1 pl-5">
        <li>
          <code>id-token: write</code> is required. Without it the job has no
          GitHub token.
        </li>
        <li>
          <code>project</code> is where hook events go. The agent may send them
          to <strong>{project.name || project.slug}</strong> only; leave it out
          to send none.
        </li>
        <li>
          Pull requests from forks get no GitHub token, so the action skips
          them.
        </li>
        <li>
          Later steps reach the agent&apos;s MCP servers through Claude Code or
          Codex. With <code>anthropics/claude-code-action</code>, keep this
          action before it in the same job.
        </li>
      </ul>
      <Links links={[{ href: ACTION_DOCS, label: "Device agent action" }]} />
    </div>
  );
}

/**
 * GitHub Actions on github.com, through the device agent.
 *
 * The job runs Speakeasy's device agent action, which exchanges the job's
 * GitHub token at the organization's device-agent endpoint, fetches the
 * agent's plugins, writes each tool's MCP servers with a per-connection token
 * helper, and sends hook events. One access rule covers the whole repository,
 * and the agent it names supplies all three, so the agent needs the device
 * agent's grants as well as its servers.
 */
export function githubActionsSetupInstructions(
  entry: CatalogEntry,
): PlatformSetup {
  const fields = ["owner", "repo"]
    .map((key) => entry.variables.find((variable) => variable.key === key))
    .filter((found): found is CatalogVariable => found !== undefined);

  return {
    agentGrants: githubActionsAgentGrants,
    connection: (values): Connection | null => {
      const rule = subjectRule(entry, values);
      if (rule === null || repositoryProblem(values) !== null) return null;
      return {
        name: entry.displayName,
        description: entry.description,
        issuer: entry.issuer.value,
        jwksUri: entry.jwksUri.value,
        subject: rule.subject,
        matchKind: rule.matchKind,
      };
    },
    requirements: (
      <div className="space-y-3 text-sm">
        <p>
          <strong>A repository on github.com</strong> whose workflows you can
          edit. GitHub Enterprise Server is not supported.
        </p>
        <p>
          <strong>
            <code>{DEVICE_AGENT_SYNC_SCOPE}</code> and{" "}
            <code>{HOOKS_INGEST_SCOPE}</code>
          </strong>{" "}
          yourself, so you can give them to the agent. Organization
          administrators have both.
        </p>
        <Links links={[{ href: OIDC_DOCS, label: "GitHub OIDC tokens" }]} />
      </div>
    ),
    steps: [
      {
        id: "repository",
        short: "Repository",
        title: "Your repository",
        phase: "create",
        complete: ({ values }) =>
          subjectRule(entry, values) !== null &&
          repositoryProblem(values) === null,
        body: ({ values, setValue, ruleConflict, agentName }) => {
          const rule = subjectRule(entry, values);
          const problem = repositoryProblem(values);
          const repository = repositoryName(values);
          const agent = agentName || "this agent";
          return (
            <>
              <p className="text-sm">
                The repository whose jobs act as {agent}. Enter the names with
                the same capital letters GitHub shows: Speakeasy compares them
                exactly.
              </p>
              {fields.map((field) => (
                <VariableField
                  key={field.key}
                  variable={field}
                  value={values[field.key] ?? ""}
                  onChange={(next) => setValue(field.key, next)}
                />
              ))}
              {problem !== null && (
                <Alert variant="error">
                  <div className="text-sm">{problem}</div>
                </Alert>
              )}
              <details className="text-sm">
                <summary className="text-muted-foreground hover:text-foreground cursor-pointer">
                  Repositories created or renamed since July 15, 2026
                </summary>
                <div className="mt-2 space-y-2">
                  <p>
                    GitHub puts the owner and repository IDs in their tokens, as{" "}
                    <code>owner@ID</code> and <code>repo@ID</code>. Other
                    repositories can opt in. The IDs also keep someone who later
                    takes over a deleted owner or repository name from matching
                    this rule. This prints the values to enter:
                  </p>
                  <CodeBlock
                    value={`gh api repos/${repository} --jq '"\\(.owner.login)@\\(.owner.id) \\(.name)@\\(.id)"'`}
                    label="Terminal"
                    language="shell"
                  />
                  <p className="text-muted-foreground">
                    If a job is refused, the action prints the subject GitHub
                    gave it.
                  </p>
                </div>
              </details>
              {rule !== null && problem === null && (
                <Alert variant="warning">
                  <div className="space-y-2 text-sm">
                    <p>
                      Every job in <strong>{repository}</strong> can sign in as{" "}
                      {agent}: every branch, tag, environment and pull request.
                      The rule is <code>{rule.subject}</code>.
                    </p>
                    <p>
                      Anyone who can push a branch to the repository can run
                      such a job and use {agent}&apos;s access. Pull requests
                      from forks get no GitHub token, so outside contributors
                      cannot. To admit only one branch or environment, use a
                      custom platform with its exact subject instead.
                    </p>
                  </div>
                </Alert>
              )}
              <p className="text-muted-foreground text-sm">
                Connecting also gives {agent}{" "}
                <code>{DEVICE_AGENT_SYNC_SCOPE}</code>, so the job can fetch its
                plugins, and <code>{HOOKS_INGEST_SCOPE}</code> for this project,
                so it can send hook events.
              </p>
              {ruleConflict !== null && (
                <Alert variant="error">
                  <div className="text-sm">{ruleConflict}</div>
                </Alert>
              )}
            </>
          );
        },
      },
      {
        id: "plugins",
        short: "Plugins",
        title: "What the job gets",
        phase: "connect",
        body: ({ agentName, servers }) => (
          <PluginsStep agentName={agentName} servers={servers} />
        ),
      },
      {
        id: "workflow",
        short: "Workflow",
        title: "Add the workflow",
        phase: "connect",
        body: () => <WorkflowStep />,
      },
      {
        id: "check",
        short: "Run it",
        title: "Check it worked",
        phase: "connect",
        body: ({ agentName }) => (
          <div className="space-y-3 text-sm">
            <p>
              Run the workflow. The <strong>Connect to Speakeasy</strong> step
              prints the agent the job signed in as, which should be{" "}
              <strong>{agentName || "this agent"}</strong>, and each tool it set
              up. <code>claude mcp list</code> then shows the agent&apos;s
              servers as connected.
            </p>
            <ul className="list-disc space-y-1 pl-5">
              <li>
                <strong>Speakeasy refused this job&apos;s GitHub token</strong>:
                compare the subject it prints with the rule from the first step,
                including capital letters and IDs.
              </li>
              <li>
                <strong>An error naming {DEVICE_AGENT_SYNC_SCOPE}</strong>: the
                agent, or its owner, lacks that permission.
              </li>
              <li>
                <strong>A server shows as needing authentication</strong>: give
                the agent that server under MCP Selection.
              </li>
              <li>
                <strong>Hook events are off</strong>: set the{" "}
                <code>project</code> input.
              </li>
            </ul>
          </div>
        ),
      },
    ],
  };
}
