import { getServerURL } from "@/lib/utils";
import { CodeBlock } from "./CodeBlock";
import {
  DEVICE_AGENT_SYNC_SCOPE,
  OIDC_DOCS,
  controlPlaneProblem,
  githubActionsAgentGrants,
  jobScope,
  NARROWER_SCOPES,
  repositoryName,
  repositoryProblem,
} from "./githubActions";
import {
  JobScopeChoice,
  PluginsStep,
  RepositoryRuleWarning,
  WorkflowStep,
} from "./GitHubActionsSteps";
import { Links, SetupError, VariableField } from "./SetupFields";
import type { Connection, PlatformSetup } from "./setupInstructions";
import {
  SUBJECT_CHOICE,
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
 * one branch or one environment, and the agent it names supplies all three, so
 * the agent needs the device agent's grants as well as its servers.
 */
export function githubActionsSetupInstructions(
  entry: CatalogEntry,
): PlatformSetup {
  const variable = (key: string) =>
    entry.variables.find((found) => found.key === key);
  const fields = ["owner", "repo"]
    .map(variable)
    .filter((found): found is CatalogVariable => found !== undefined);
  // The scopes this setup can write a workflow for, labelled by the catalog.
  // The default rule is the whole repository, which the values leave unset.
  const choices = [
    {
      key: "repository",
      label: entry.subject.label ?? "Whole repository",
    },
    ...entry.subject.alternatives.filter((choice) =>
      NARROWER_SCOPES.some((scope) => scope === choice.key),
    ),
  ];
  // A deployment the action cannot reach gets no rule and no grants: they
  // would trust GitHub for the organization with nothing able to use them.
  const unreachable = controlPlaneProblem(getServerURL());

  return {
    // The device agent exchanges the job's token at each server itself.
    ownTokenExchange: true,
    // Nothing is written for an unreachable deployment, so no grant is shown
    // in the summary or checked either.
    agentGrants: (organizationId, projectId) =>
      unreachable === null
        ? githubActionsAgentGrants(organizationId, projectId)
        : [],
    keywords: ["GitHub", "CI", "workflow", "device agent"],
    connection: (values): Connection | null => {
      const rule = subjectRule(entry, values);
      if (
        rule === null ||
        repositoryProblem(values) !== null ||
        unreachable !== null
      ) {
        return null;
      }
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
            Permission to sync device agents and to send hook events
          </strong>{" "}
          yourself, so you can give both to the agent. Organization
          administrators have them.
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
          unreachable === null &&
          subjectRule(entry, values) !== null &&
          repositoryProblem(values) === null,
        body: ({ values, setValue, ruleConflict, agentName }) => {
          const rule = subjectRule(entry, values);
          const problem = repositoryProblem(values);
          const repository = repositoryName(values);
          const agent = agentName || "this agent";
          const scope = jobScope(values);
          const scopeField =
            scope.kind === "repository" ? undefined : variable(scope.kind);
          return (
            <>
              {unreachable !== null && <SetupError>{unreachable}</SetupError>}
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
              {problem !== null && <SetupError>{problem}</SetupError>}
              <JobScopeChoice
                choices={choices}
                value={scope.kind}
                onChange={(key) =>
                  setValue(SUBJECT_CHOICE, key === "repository" ? "" : key)
                }
              />
              {scopeField !== undefined && (
                <VariableField
                  variable={scopeField}
                  value={values[scopeField.key] ?? ""}
                  onChange={(next) => setValue(scopeField.key, next)}
                />
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
                <RepositoryRuleWarning
                  scope={scope}
                  repository={repository}
                  agent={agent}
                  subject={rule.subject}
                />
              )}
              <p className="text-muted-foreground text-sm">
                Connecting also lets {agent} fetch its plugins and send hook
                events to this project.
              </p>
              {ruleConflict !== null && <SetupError>{ruleConflict}</SetupError>}
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
        body: ({ values }) => <WorkflowStep scope={jobScope(values)} />,
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
                including capital letters and IDs. A job that uses an
                environment is named by the environment, not its branch.
              </li>
              <li>
                <strong>
                  An error naming <code>{DEVICE_AGENT_SYNC_SCOPE}</code>
                </strong>
                : the agent, or its owner, lacks permission to sync device
                agents.
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
