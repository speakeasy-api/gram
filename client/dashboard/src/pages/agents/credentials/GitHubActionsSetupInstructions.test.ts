import type { Plugin } from "@gram/client/models/components/plugin.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  ACTION_DEFAULT_CONTROL_PLANE,
  controlPlaneProblem,
  githubActionsAgentGrants,
  jobScope,
  pluginGateways,
  pluginServersWithoutAuthorization,
  pluginsForEveryAgent,
  repositoryProblem,
  unreachablePluginServers,
  workflowYaml,
  type JobScope,
  type WorkflowAgent,
} from "./githubActions";
import { githubActionsSetupInstructions } from "./GitHubActionsSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import {
  SUBJECT_CHOICE,
  subjectRule,
  valuesFromSubject,
  type CatalogEntry,
} from "./workloadCatalog";

const server = vi.hoisted(() => ({ url: "" }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => server.url,
}));
afterEach(() => {
  server.url = "";
});

// Patterns copied from
// server/internal/workloadpolicy/catalog/platforms/github-actions.yaml.
const github: CatalogEntry = {
  key: "github-actions",
  displayName: "GitHub Actions",
  description: "",
  enabled: true,
  issuer: {
    value: "https://token.actions.githubusercontent.com",
    visibility: "read-only",
  },
  jwksUri: {
    value: "https://token.actions.githubusercontent.com/.well-known/jwks",
    visibility: "hidden",
  },
  variables: [
    {
      key: "owner",
      tier: "rule",
      label: "Owner",
      pattern: "[A-Za-z0-9][A-Za-z0-9-]{0,38}(?:@[0-9]+)?",
    },
    {
      key: "repo",
      tier: "rule",
      label: "Repository",
      pattern: "[A-Za-z0-9._-]{1,100}(?:@[0-9]+)?",
    },
    {
      key: "branch",
      tier: "rule",
      label: "Branch",
      pattern:
        "[^\\x00-\\x20\\x7f/:*?\\[\\\\^~]+(?:/[^\\x00-\\x20\\x7f/:*?\\[\\\\^~]+)*",
    },
    {
      key: "environment",
      tier: "rule",
      label: "Environment",
      pattern: "[^:*\\x00-\\x1f\\x7f]{1,255}",
    },
  ],
  subject: {
    template: "repo:{owner}/{repo}:",
    wildcard: true,
    label: "Whole repository",
    alternatives: [
      {
        key: "branch",
        label: "One branch",
        template: "repo:{owner}/{repo}:ref:refs/heads/{branch}",
        wildcard: false,
      },
      {
        key: "environment",
        label: "One environment",
        template: "repo:{owner}/{repo}:environment:{environment}",
        wildcard: false,
      },
    ],
  },
};

describe("GitHub Actions setup", () => {
  it("is offered as a guided setup", () => {
    expect(hasPlatformSetup(github)).toBe(true);
  });

  it("admits the whole repository with one wildcard rule", () => {
    const setup = githubActionsSetupInstructions(github);
    expect(setup.connection({ owner: "acme", repo: "payments-api" })).toEqual({
      name: "GitHub Actions",
      description: "",
      issuer: "https://token.actions.githubusercontent.com",
      jwksUri: "https://token.actions.githubusercontent.com/.well-known/jwks",
      subject: "repo:acme/payments-api:*",
      matchKind: "wildcard",
    });
  });

  it("keeps capital letters and immutable IDs as entered", () => {
    expect(
      subjectRule(github, { owner: "Acme@123", repo: "Payments.API@456" }),
    ).toEqual({
      subject: "repo:Acme@123/Payments.API@456:*",
      matchKind: "wildcard",
    });
  });

  it("refuses values that would move the stem", () => {
    for (const values of [
      { owner: "acme/evil", repo: "app" },
      { owner: "acme", repo: "app:ref" },
      { owner: "acme", repo: "app*" },
      { owner: "acme@x", repo: "app" },
    ]) {
      expect(subjectRule(github, values)).toBeNull();
    }
  });

  it("admits one branch or one environment with an exact rule", () => {
    const setup = githubActionsSetupInstructions(github);
    const repository = { owner: "acme", repo: "payments-api" };
    expect(
      setup.connection({
        ...repository,
        [SUBJECT_CHOICE]: "branch",
        branch: "release/v2",
      }),
    ).toMatchObject({
      subject: "repo:acme/payments-api:ref:refs/heads/release/v2",
      matchKind: "exact",
    });
    expect(
      setup.connection({
        ...repository,
        [SUBJECT_CHOICE]: "environment",
        environment: "Production EU",
      }),
    ).toMatchObject({
      subject: "repo:acme/payments-api:environment:Production EU",
      matchKind: "exact",
    });
    // The other choice's value is not part of the rule, and the chosen one
    // must be filled in.
    expect(
      setup.connection({
        ...repository,
        [SUBJECT_CHOICE]: "environment",
        branch: "main",
      }),
    ).toBeNull();
    expect(
      subjectRule(github, {
        ...repository,
        [SUBJECT_CHOICE]: "",
        branch: "main",
      }),
    ).toEqual({ subject: "repo:acme/payments-api:*", matchKind: "wildcard" });
  });

  it("refuses a choice the catalog does not offer rather than widen the rule", () => {
    expect(
      subjectRule(github, {
        owner: "acme",
        repo: "app",
        [SUBJECT_CHOICE]: "tag",
      }),
    ).toBeNull();
  });

  it("refuses a branch or environment that would forge a context", () => {
    const repository = { owner: "acme", repo: "app" };
    const forged: Record<string, string>[] = [
      { [SUBJECT_CHOICE]: "branch", branch: "main:x" },
      { [SUBJECT_CHOICE]: "branch", branch: "release/*" },
      { [SUBJECT_CHOICE]: "branch", branch: "main/" },
      { [SUBJECT_CHOICE]: "environment", environment: "prod:x" },
      { [SUBJECT_CHOICE]: "environment", environment: "prod*" },
    ];
    for (const values of forged) {
      expect(subjectRule(github, { ...repository, ...values })).toBeNull();
    }
  });

  it("reads each choice back out of the rule it made", () => {
    expect(
      valuesFromSubject(github, {
        subject: "repo:acme/payments-api:ref:refs/heads/a/b",
        matchKind: "exact",
      }),
    ).toEqual({
      owner: "acme",
      repo: "payments-api",
      branch: "a/b",
      [SUBJECT_CHOICE]: "branch",
    });
    expect(
      valuesFromSubject(github, {
        subject: "repo:acme@1/payments-api@2:environment:review/pr-12",
        matchKind: "exact",
      }),
    ).toEqual({
      owner: "acme@1",
      repo: "payments-api@2",
      environment: "review/pr-12",
      [SUBJECT_CHOICE]: "environment",
    });
    expect(
      valuesFromSubject(github, {
        subject: "repo:acme/payments-api:*",
        matchKind: "wildcard",
      }),
    ).toEqual({
      owner: "acme",
      repo: "payments-api",
    });
    // A context the setup never writes is not read as one of its choices.
    expect(
      valuesFromSubject(github, {
        subject: "repo:acme/payments-api:pull_request",
        matchKind: "exact",
      }),
    ).toEqual({});
  });

  it("refuses an ID on only one of the two names", () => {
    const setup = githubActionsSetupInstructions(github);
    expect(
      repositoryProblem({ owner: "acme@123", repo: "app" }),
    ).not.toBeNull();
    expect(
      repositoryProblem({ owner: "acme", repo: "app@456" }),
    ).not.toBeNull();
    expect(
      repositoryProblem({ owner: "acme@123", repo: "app@456" }),
    ).toBeNull();
    expect(setup.connection({ owner: "acme@123", repo: "app" })).toBeNull();
  });

  it("gives no grants to show or check while the deployment is unreachable", () => {
    server.url = "http://speakeasy.example.com";
    const setup = githubActionsSetupInstructions(github);
    expect(setup.agentGrants?.("org-1", "project-1")).toEqual([]);
    expect(setup.connection({ owner: "acme", repo: "app" })).toBeNull();
  });

  it("gives the agent device sync and hooks for one project", () => {
    const setup = githubActionsSetupInstructions(github);
    expect(setup.agentGrants?.("org-1", "project-1")).toEqual(
      githubActionsAgentGrants("org-1", "project-1"),
    );
    expect(githubActionsAgentGrants("org-1", "project-1")).toEqual([
      {
        effect: "allow",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "*" },
      },
      {
        effect: "allow",
        scope: "org:hooks_ingest",
        selector: {
          resourceKind: "org",
          resourceId: "org-1",
          projectId: "project-1",
        },
      },
    ]);
  });
});

describe("workflowYaml", () => {
  const base = {
    agent: "claude" as WorkflowAgent,
    organizationId: "org-123",
    projectSlug: "ci",
    controlPlaneUrl: ACTION_DEFAULT_CONTROL_PLANE,
    scope: { kind: "repository" } as JobScope,
  };

  /** The workflow for these options, which must have one. */
  function workflow(options: Partial<typeof base> = {}): string {
    const { yaml } = workflowYaml({ ...base, ...options });
    if (yaml === null) throw new Error("expected a workflow");
    return yaml;
  }

  const scopes: JobScope[] = [
    { kind: "repository" },
    { kind: "branch", name: "release/v2" },
    { kind: "environment", name: "production" },
  ];

  /** Every variant the setup can print. */
  const variants = (["claude", "codex"] as const).flatMap((agent) =>
    [ACTION_DEFAULT_CONTROL_PLANE, "https://speakeasy.example.com"].flatMap(
      (controlPlaneUrl) =>
        scopes.map((scope) => workflow({ agent, controlPlaneUrl, scope })),
    ),
  );

  it("fills in the organization and the hook events project", () => {
    const yaml = workflow();
    expect(yaml).toContain("id-token: write");
    expect(yaml).toContain("organization-id: org-123");
    expect(yaml).toContain("project: ci");
    expect(yaml).not.toContain("control-plane-url");
  });

  // The job grants id-token: write, so every step can mint its OIDC token: no
  // action or CLI may come from a branch or tag that could move. speakeasyd is
  // the deliberate exception: the device agent action installs the latest
  // stable release unless the step sets version, so a job picks up a fix
  // without its workflow being edited, and it checks every download, pinned
  // or not, against the stable release manifest in the same bucket. That
  // catches a download corrupted or tampered with in transit, not a
  // compromised bucket.
  it("pins every action and CLI", () => {
    for (const yaml of variants) {
      const used = yaml.match(/uses: \S+/g) ?? [];
      expect(used.length).toBeGreaterThan(0);
      for (const step of used) {
        expect(step).toMatch(/@[0-9a-f]{40}$/);
      }
      const installed = yaml.match(/npm install -g \S+/g) ?? [];
      expect(installed).toHaveLength(1);
      for (const install of installed) {
        expect(install).toMatch(/@\d+\.\d+\.\d+$/);
      }
    }
  });

  it("runs Claude after Speakeasy has configured its MCP servers", () => {
    const yaml = workflow();
    expect(yaml).toMatch(
      /uses: anthropics\/claude-code-action@[0-9a-f]{40} # v1/,
    );
    expect(yaml).toContain(
      "anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}",
    );
    expect(yaml).toContain('mkdir -p "$HOME/.claude"');
    expect(yaml).toContain("run: claude mcp list");
    expect(yaml).toContain("persist-credentials: false");
    expect(yaml.indexOf("mkdir -p")).toBeLessThan(
      yaml.indexOf("uses: speakeasy-api/device-agent-action"),
    );
    expect(
      yaml.indexOf("uses: speakeasy-api/device-agent-action"),
    ).toBeLessThan(yaml.indexOf("uses: anthropics/claude-code-action"));
    expect(yaml).not.toContain("OPENAI_API_KEY");
    expect(yaml).not.toContain("openai/codex-action");
  });

  it("shares Codex's configuration between Speakeasy and the official action", () => {
    const yaml = workflow({ agent: "codex" });
    expect(yaml).toContain(
      'echo "CODEX_HOME=$RUNNER_TEMP/speakeasy-codex" >> "$GITHUB_ENV"',
    );
    expect(yaml).toContain('mkdir -p "$RUNNER_TEMP/speakeasy-codex"');
    expect(yaml).toContain("npm install -g @openai/codex");
    expect(yaml).toContain("run: codex mcp list");
    expect(yaml).toMatch(/uses: openai\/codex-action@[0-9a-f]{40} # v1/);
    expect(yaml).toContain("openai-api-key: ${{ secrets.OPENAI_API_KEY }}");
    expect(yaml).toContain("codex-home: ${{ env.CODEX_HOME }}");
    expect(yaml.indexOf("GITHUB_ENV")).toBeLessThan(
      yaml.indexOf("uses: speakeasy-api/device-agent-action"),
    );
    expect(
      yaml.indexOf("uses: speakeasy-api/device-agent-action"),
    ).toBeLessThan(yaml.indexOf("uses: openai/codex-action"));
    expect(yaml).not.toContain("ANTHROPIC_API_KEY");
    expect(yaml).not.toContain("anthropics/claude-code-action");
  });

  // GitHub evaluates a job's own keys before any runner exists, so the
  // runner context is only available inside steps. A runner.temp in a
  // job-level env makes GitHub reject the whole workflow.
  it("uses the runner context only inside steps", () => {
    for (const yaml of variants) {
      const steps = yaml.indexOf("    steps:");
      expect(steps).toBeGreaterThan(0);
      expect(yaml.slice(0, steps)).not.toMatch(/\$\{\{\s*(runner|steps)\./);
      expect(yaml.slice(0, steps)).not.toMatch(/^ {4}env:/m);
    }
  });

  it("runs a narrower rule's workflow only where the rule admits its jobs", () => {
    const branch = workflow({ scope: { kind: "branch", name: "release/v2" } });
    expect(branch).toContain('    branches: ["release/v2"]');
    // Glob syntax git allows in a name is escaped, so the filter matches the
    // branch alone: YAML reads "\\+" as "\+".
    expect(workflow({ scope: { kind: "branch", name: "!feat+x]" } })).toContain(
      '    branches: ["\\\\!feat\\\\+x]"]',
    );
    expect(branch).not.toContain("pull_request");
    expect(branch).not.toMatch(/^ {4}environment:/m);

    const environment = workflow({
      scope: { kind: "environment", name: "Production EU" },
    });
    expect(environment).toMatch(/^ {4}environment: "Production EU" #/m);
    expect(environment).not.toContain("pull_request");
    // The environment is the job's own key, before any step runs.
    expect(environment.indexOf("    environment:")).toBeLessThan(
      environment.indexOf("    steps:"),
    );
  });

  it("reads the scope from the setup's values", () => {
    expect(jobScope({})).toEqual({ kind: "repository" });
    expect(jobScope({ [SUBJECT_CHOICE]: "", branch: "main" })).toEqual({
      kind: "repository",
    });
    expect(
      jobScope({ [SUBJECT_CHOICE]: "branch", branch: " release/v2 " }),
    ).toEqual({ kind: "branch", name: "release/v2" });
    expect(
      jobScope({ [SUBJECT_CHOICE]: "environment", environment: "prod" }),
    ).toEqual({ kind: "environment", name: "prod" });
  });

  it("skips fork pull requests before the job starts", () => {
    for (const yaml of variants.filter((v) => v.includes("pull_request:"))) {
      const lines = yaml.split("\n");
      const job = lines.indexOf("  agent:");
      expect(job).toBeGreaterThan(0);
      // The job's first key, so no step of it runs for a fork.
      expect(lines[job + 1]).toBe(
        "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.fork == false",
      );
      expect(lines.indexOf("    runs-on: ubuntu-latest")).toBe(job + 2);
    }
  });

  it("names a deployment other than the action's default", () => {
    const yaml = workflow({
      controlPlaneUrl: "https://speakeasy.example.com/",
    });
    expect(yaml).toContain("control-plane-url: https://speakeasy.example.com");
  });

  it("prints no workflow for a control plane served without https", () => {
    const { yaml, problem } = workflowYaml({
      ...base,
      controlPlaneUrl: "http://speakeasy.example.com",
    });
    expect(yaml).toBeNull();
    expect(problem).toContain("https");
    expect(controlPlaneProblem("https://speakeasy.example.com")).toBeNull();
  });

  it("installs speakeasyd with the device agent action alone", () => {
    for (const yaml of variants) {
      expect(yaml).toContain(
        "uses: speakeasy-api/device-agent-action@596e9f8598bf2896e6dda3c45eb098313b671962 # main",
      );
      // The only checkout is the repository's own, and nothing builds
      // speakeasyd before the action installs a release.
      expect(yaml.match(/uses: actions\/checkout@/g)).toHaveLength(1);
      expect(yaml).not.toContain("actions/setup-go");
      expect(yaml).not.toContain("go build");
      expect(yaml).not.toContain("speakeasyd-binary");
      // No version input, so the action installs the latest stable release.
      expect(yaml).not.toMatch(/^\s+version:/m);
    }
  });

  it("shows placeholders while the organization or project is unknown", () => {
    const yaml = workflow({ organizationId: "", projectSlug: "" });
    expect(yaml).toContain("organization-id: SPEAKEASY_ORGANIZATION_ID");
    expect(yaml).toContain("project: SPEAKEASY_PROJECT");
  });
});

function plugin(
  id: string,
  assignments: {
    principalUrn: string;
    installMode: "required" | "default" | "available";
  }[],
  servers: {
    displayName: string;
    toolsetId?: string;
    mcpServerId?: string;
    metaMcpServerId?: string;
  }[],
): Plugin {
  return {
    id,
    name: id,
    agentPluginsV1Compatible: true,
    autoCreated: false,
    createdAt: new Date(0),
    assignments: assignments.map((assignment, index) => ({
      id: `${id}-${index}`,
      createdAt: new Date(0),
      ...assignment,
    })),
    servers: servers.map((server, index) => ({
      id: `${id}-server-${index}`,
      createdAt: new Date(0),
      policy: "required",
      sortOrder: index,
      ...server,
    })),
  } as Plugin;
}

describe("plugins a job receives", () => {
  const everyone = plugin(
    "observability",
    [{ principalUrn: "*", installMode: "required" }],
    [
      { displayName: "Linear", toolsetId: "toolset-linear" },
      { displayName: "Gateway", metaMcpServerId: "gateway-1" },
    ],
  );
  const optional = plugin(
    "optional",
    [{ principalUrn: "*", installMode: "available" }],
    [],
  );
  const roleOnly = plugin(
    "role",
    [{ principalUrn: "role:organization:1", installMode: "default" }],
    [],
  );

  it("are the ones installed for everyone", () => {
    expect(
      pluginsForEveryAgent([everyone, optional, roleOnly]).map((p) => p.id),
    ).toEqual(["observability"]);
  });

  it("match a server that has only an MCP server id", () => {
    const remote = plugin(
      "remote",
      [{ principalUrn: "*", installMode: "required" }],
      [
        { displayName: "Reachable", mcpServerId: "mcp-reachable" },
        { displayName: "Unreachable", mcpServerId: "mcp-unreachable" },
      ],
    );
    expect(
      unreachablePluginServers(
        [remote],
        [
          {
            id: "server-2",
            resourceId: "mcp-reachable",
            name: "Reachable",
            mcpUrl: "https://x",
          },
        ],
        [],
      ),
    ).toEqual(["Unreachable"]);
  });

  it("name the servers the agent cannot use", () => {
    expect(unreachablePluginServers([everyone], [], [])).toEqual(["Linear"]);
    expect(
      unreachablePluginServers(
        [everyone],
        [
          {
            id: "server-1",
            resourceId: "toolset-linear",
            name: "Linear",
            mcpUrl: "https://x",
          },
        ],
        [],
      ),
    ).toEqual([]);
  });

  it("set gateways apart, since a job skips them", () => {
    expect(pluginGateways([everyone])).toEqual(["Gateway"]);
    expect(unreachablePluginServers([everyone], [], [])).not.toContain(
      "Gateway",
    );
  });

  it("set apart servers with no Speakeasy authorization server, since a job skips them too", () => {
    const mixed = plugin(
      "mixed",
      [{ principalUrn: "*", installMode: "required" }],
      [
        { displayName: "Signed in", mcpServerId: "mcp-issuer" },
        { displayName: "Legacy OAuth", toolsetId: "toolset-legacy" },
        { displayName: "Unknown", mcpServerId: "mcp-missing" },
      ],
    );
    const rows = [
      { id: "mcp-issuer", userSessionIssuerId: "issuer-1" },
      // A gateway member's wrapper names the toolset too, with an issuer of
      // its own, but it is not the server the toolset is reached at.
      {
        id: "member-wrapper",
        toolsetId: "toolset-legacy",
        userSessionIssuerId: "issuer-2",
        toolset: { hostingMcpServerId: "mcp-legacy" },
      },
      {
        id: "mcp-legacy",
        toolsetId: "toolset-legacy",
        toolset: { hostingMcpServerId: "mcp-legacy" },
      },
    ] as McpServer[];
    expect(pluginServersWithoutAuthorization([mixed], rows)).toEqual([
      "Legacy OAuth",
    ]);
    // Giving the agent a server it skips would not help, so it is not named
    // as one to add; a server the list does not have is not guessed at.
    expect(unreachablePluginServers([mixed], [], rows)).toEqual([
      "Signed in",
      "Unknown",
    ]);
  });
});
