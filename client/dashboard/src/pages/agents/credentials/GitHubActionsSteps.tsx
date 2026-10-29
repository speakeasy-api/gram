import { Alert } from "@/components/ui/Alert";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useQueries } from "@tanstack/react-query";
import type { JSX } from "react";
import { Link } from "react-router";
import { CodeBlock } from "./CodeBlock";
import {
  ACTION_DOCS,
  pluginsForEveryAgent,
  unreachablePluginServers,
  workflowYaml,
} from "./githubActions";
import { Links } from "./SetupFields";
import type { SetupServer } from "./setupInstructions";

export function PluginsStep({
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

export function WorkflowStep(): JSX.Element {
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
