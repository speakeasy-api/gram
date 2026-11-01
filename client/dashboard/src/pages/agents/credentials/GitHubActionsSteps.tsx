import { Alert } from "@/components/ui/Alert";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Skeleton } from "@/components/ui/Skeleton";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useQueries } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import { Link } from "react-router";
import { CodeBlock } from "./CodeBlock";
import { PanelTab, PanelTabs } from "./PanelTabs";
import {
  ACTION_DOCS,
  WORKFLOW_AGENTS,
  WORKFLOW_AGENT_OPTIONS,
  pluginGateways,
  pluginsForEveryAgent,
  unreachablePluginServers,
  workflowYaml,
  type WorkflowAgent,
} from "./githubActions";
import { Links, SetupError } from "./SetupFields";
import type { SetupServer } from "./setupInstructions";

/** How long a plugin read for this step is reused before it is read again. */
const PLUGIN_STALE_TIME_MS = 30_000;

export function PluginsStep({
  agentName,
  servers,
}: {
  agentName: string;
  servers: SetupServer[];
}): JSX.Element {
  const project = useProject();
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
      staleTime: PLUGIN_STALE_TIME_MS,
    })),
  });
  const pending = plugins.isPending || details.some((query) => query.isPending);
  const failed = plugins.isError || details.some((query) => query.isError);
  const delivered = pluginsForEveryAgent(
    details.flatMap((query) => (query.data === undefined ? [] : [query.data])),
  );
  const unreachable = unreachablePluginServers(delivered, servers);
  const gateways = pluginGateways(delivered);
  const agent = agentName || "the agent";
  const them = unreachable.length === 1 ? "it" : "them";

  return (
    <div className="space-y-4 text-sm">
      <p>
        The job gets {agent}&apos;s plugins: their MCP servers, skills and
        hooks. Speakeasy also adds its observability plugin, which sends the
        job&apos;s hook events.
      </p>
      <p className="text-muted-foreground">
        Below are the plugins installed for <strong>Everyone</strong> in this
        project. The list is partial: the job also gets plugins assigned to
        roles {agent} holds, and those from other projects.
      </p>
      <DeliveredPlugins
        pending={pending}
        failed={failed}
        delivered={delivered}
        agent={agent}
      />
      {gateways.length > 0 && (
        <p className="text-muted-foreground">
          The job skips {gateways.join(", ")}: gateways do not accept agents
          yet.
        </p>
      )}
      {unreachable.length > 0 && (
        <Alert variant="warning">
          <div className="text-sm">
            {agentName || "The agent"} cannot use {unreachable.join(", ")}, so
            the job&apos;s tools will show {them} as needing authentication. Go
            back to MCP Selection to add {them}.
          </div>
        </Alert>
      )}
      <p className="text-muted-foreground">
        Servers that need each person to sign in do not work in a job: there is
        nobody to sign in.
      </p>
    </div>
  );
}

/**
 * The plugins assigned to Everyone, or why there are none to show. Reading is
 * shown in the list's own shape, so the step does not jump once it loads.
 */
function DeliveredPlugins({
  pending,
  failed,
  delivered,
  agent,
}: {
  pending: boolean;
  failed: boolean;
  delivered: Plugin[];
  agent: string;
}): JSX.Element {
  const routes = useRoutes();
  const pluginsPage = (
    <Link className="underline" to={routes.plugins.href()}>
      Plugins page
    </Link>
  );
  if (pending) {
    return (
      <div
        role="status"
        className="border-border space-y-2 border px-3 py-2.5"
        aria-label="Reading this project's plugins"
      >
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-4 w-56" />
      </div>
    );
  }
  if (failed) {
    return (
      <Alert variant="warning">
        <div className="text-sm">
          This project&apos;s plugins could not be read. Check them on the{" "}
          {pluginsPage}.
        </div>
      </Alert>
    );
  }
  if (delivered.length === 0) {
    return (
      <Alert variant="warning">
        <div className="text-sm">
          No plugin in this project is installed for Everyone, so unless {agent}{" "}
          holds a role with plugins, the job gets no MCP servers. A plugin that
          is only Available to Everyone is not installed on a job. On the{" "}
          {pluginsPage}, assign a plugin to <strong>Everyone</strong> as
          Required or On by default, or to a role and give the agent that role.
        </div>
      </Alert>
    );
  }
  return (
    <ul className="border-border divide-border divide-y border">
      {delivered.map((plugin) => {
        const servers = (plugin.servers ?? []).map(
          (server) => server.displayName,
        );
        return (
          <li key={plugin.id} className="px-3 py-2.5">
            <p className="font-medium">{plugin.name}</p>
            <p className="text-muted-foreground">
              {servers.length === 0 ? "No MCP servers" : servers.join(", ")}
            </p>
          </li>
        );
      })}
    </ul>
  );
}

export function WorkflowStep(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const [agent, setAgent] = useState<WorkflowAgent>("claude");
  const selected = WORKFLOW_AGENTS[agent];
  const workflow = workflowYaml({
    agent,
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
      <PanelTabs
        aria-label="Workflow agent"
        value={agent}
        onValueChange={setAgent}
        tabs={WORKFLOW_AGENT_OPTIONS.map((option) => ({
          value: option.id,
          label: (
            <span className="inline-flex items-center gap-2">
              <AgentProviderIcon
                source={option.iconSource}
                className="size-3.5"
              />
              {option.name}
            </span>
          ),
        }))}
      >
        {/* One panel, for the chosen agent: the other tab's workflow would
            be a second copy of the same file under the same name. */}
        <PanelTab value={agent}>
          <p>
            Add <code>{selected.secret}</code> to your repository&apos;s Actions
            secrets for {selected.name}, then replace the example prompt with
            your task. Speakeasy access uses the job&apos;s GitHub token
            separately.
          </p>
          {workflow.yaml === null ? (
            <SetupError>{workflow.problem}</SetupError>
          ) : (
            <CodeBlock
              value={workflow.yaml}
              label=".github/workflows/speakeasy.yml"
              language="yaml"
            />
          )}
          <Alert variant="info">
            <div className="space-y-2 text-sm">
              <p>
                <strong>Preview.</strong> The action needs a speakeasyd with
                GitHub Actions support, which no release includes yet, so this
                workflow builds speakeasyd from source. Remove those three steps
                and the <code>speakeasyd-binary</code> input once a release
                supports it.
              </p>
            </div>
          </Alert>
          <ul className="list-disc space-y-1 pl-5">
            <li>
              <code>id-token: write</code> is required. Without it the job has
              no GitHub token.
            </li>
            <li>
              <code>project</code> is where hook events go. The agent may send
              them to <strong>{project.name || project.slug}</strong> only;
              leave it out to send none.
            </li>
            <li>
              On <code>pull_request</code>, forks get no GitHub token, and the
              job&apos;s <code>if:</code> skips them. Don&apos;t run it on{" "}
              <code>pull_request_target</code>: that event gives a fork&apos;s
              pull request a token the rule admits.
            </li>
            <li>
              Keep <strong>Connect to Speakeasy</strong> before{" "}
              <strong>Run {selected.name}</strong> in the same job so it can use
              the configured MCP servers.
            </li>
          </ul>
          <Links
            links={[
              { href: ACTION_DOCS, label: "Device agent action" },
              { href: selected.docs, label: `${selected.name} action` },
            ]}
          />
        </PanelTab>
      </PanelTabs>
    </div>
  );
}
