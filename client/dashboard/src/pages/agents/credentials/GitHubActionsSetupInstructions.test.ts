import type { Plugin } from "@gram/client/models/components/plugin.js";
import { describe, expect, it } from "vitest";
import {
  ACTION_DEFAULT_CONTROL_PLANE,
  githubActionsAgentGrants,
  pluginsForEveryAgent,
  repositoryProblem,
  unreachablePluginServers,
  workflowYaml,
} from "./githubActions";
import { githubActionsSetupInstructions } from "./GitHubActionsSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import { subjectRule, type CatalogEntry } from "./workloadCatalog";

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
  ],
  subject: { template: "repo:{owner}/{repo}:", wildcard: true },
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

  it("gives the agent device sync and hooks for one project", () => {
    const setup = githubActionsSetupInstructions(github);
    expect(setup.agentGrants?.("project-1")).toEqual(
      githubActionsAgentGrants("project-1"),
    );
    expect(githubActionsAgentGrants("project-1")).toEqual([
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
          resourceId: "*",
          projectId: "project-1",
        },
      },
    ]);
  });
});

describe("workflowYaml", () => {
  const base = {
    organizationId: "org-123",
    projectSlug: "ci",
    controlPlaneUrl: ACTION_DEFAULT_CONTROL_PLANE,
    buildFromSource: false,
  };

  it("fills in the organization and the hook events project", () => {
    const yaml = workflowYaml(base);
    expect(yaml).toContain("id-token: write");
    expect(yaml).toContain("uses: speakeasy-api/device-agent-action@main");
    expect(yaml).toContain("organization-id: org-123");
    expect(yaml).toContain("project: ci");
    expect(yaml).not.toContain("control-plane-url");
    expect(yaml).not.toContain("speakeasyd-binary");
  });

  it("names a deployment other than the action's default", () => {
    const yaml = workflowYaml({
      ...base,
      controlPlaneUrl: "https://speakeasy.example.com/",
    });
    expect(yaml).toContain("control-plane-url: https://speakeasy.example.com");
  });

  it("builds speakeasyd from source until a release supports it", () => {
    const yaml = workflowYaml({ ...base, buildFromSource: true });
    expect(yaml).toContain("repository: speakeasy-api/device-agent");
    expect(yaml).toContain('go build -o "$RUNNER_TEMP/speakeasyd"');
    expect(yaml).toContain("speakeasyd-binary: ${{ runner.temp }}/speakeasyd");
  });

  it("shows placeholders while the organization or project is unknown", () => {
    const yaml = workflowYaml({ ...base, organizationId: "", projectSlug: "" });
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

  it("name the servers the agent cannot use", () => {
    expect(unreachablePluginServers([everyone], [])).toEqual(["Linear"]);
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
      ),
    ).toEqual([]);
  });
});
