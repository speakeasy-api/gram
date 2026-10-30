import { Alert } from "@/components/ui/Alert";
import { HOOKS_INGEST_SCOPE } from "../agent-hook-events";
import { CodeBlock } from "./CodeBlock";
import {
  DEVICE_AGENT_SYNC_SCOPE,
  OIDC_DOCS,
  githubActionsAgentGrants,
  repositoryName,
  repositoryProblem,
} from "./githubActions";
import { PluginsStep, WorkflowStep } from "./GitHubActionsSteps";
import { Links, VariableField } from "./SetupFields";
import type { Connection, PlatformSetup } from "./setupInstructions";
import {
  subjectRule,
  type CatalogEntry,
  type CatalogVariable,
} from "./workloadCatalog";

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
                  Repositories created, renamed or transferred since July 15,
                  2026
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
                      such a job and use {agent}&apos;s access. On the{" "}
                      <code>pull_request</code> event, pull requests from forks
                      get no GitHub token, so outside contributors cannot. A{" "}
                      <code>pull_request_target</code> workflow with{" "}
                      <code>id-token: write</code> is admitted too, and if it
                      checks out the pull request&apos;s head, code from a fork
                      runs as {agent}. To admit only one branch or environment,
                      use a custom platform with its exact subject instead.
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
              up. The workflow then lists the configured MCP servers and runs
              your chosen agent.
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
