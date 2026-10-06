import { describe, expect, it } from "vitest";

import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

import { buildRequestedGrants } from "../agent-api-key-grants";

import {
  agentPurposeFromPolicy,
  deviceAgentPolicyGrants,
  deviceAgentPurposeBlocked,
  missingPolicyGrants,
  policyConnectsToMCP,
  selectDeviceAgentKeyGrants,
  undelegableScopesMessage,
  reviewCommands,
} from "./device-agent";

const PROJECT = "proj-1";
const required = deviceAgentPolicyGrants(PROJECT);

function stored(form: AgentPolicyGrantForm, id: string): AgentPolicyGrant {
  return { id, ...form } as AgentPolicyGrant;
}

describe("device agent policy grants", () => {
  it("requires sync and hooks on the org and read on the project", () => {
    expect(required.map((g) => [g.scope, g.selector])).toEqual([
      ["org:device_agent_sync", { resourceKind: "org", resourceId: "*" }],
      ["org:hooks_ingest", { resourceKind: "org", resourceId: "*" }],
      ["project:read", { resourceKind: "project", resourceId: PROJECT }],
    ]);
  });

  it("skips grants the policy already covers, including broader ones", () => {
    const broadProject = stored(
      {
        effect: "allow",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "*" },
      },
      "g1",
    );
    const sync = stored(required[0]!, "g2");
    expect(
      missingPolicyGrants([broadProject, sync], required).map((g) => g.scope),
    ).toEqual(["org:hooks_ingest"]);
  });

  it("does not treat another project as covering the chosen one", () => {
    const other = stored(
      {
        effect: "allow",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "proj-2" },
      },
      "g1",
    );
    expect(
      missingPolicyGrants([other], required).map((g) => g.scope),
    ).toContain("project:read");
  });
});

describe("device agent key grants", () => {
  it("narrows a wildcard project candidate to the chosen project", () => {
    const delegable: AgentPolicyGrantForm[] = [
      required[0]!,
      required[1]!,
      {
        effect: "allow",
        scope: "project:read",
        selector: { resourceKind: "project", resourceId: "*" },
      },
    ];
    const { selections, missingScopes } = selectDeviceAgentKeyGrants(
      delegable,
      required,
    );
    expect(missingScopes).toEqual([]);
    expect(buildRequestedGrants(selections)).toEqual(required);
  });

  it("specializes a candidate that wildcards the resource kind", () => {
    const delegable: AgentPolicyGrantForm[] = [
      required[0]!,
      required[1]!,
      {
        effect: "allow",
        scope: "project:read",
        selector: { resourceKind: "*", resourceId: "*" },
      },
    ];
    const { selections, missingScopes } = selectDeviceAgentKeyGrants(
      delegable,
      required,
    );
    expect(missingScopes).toEqual([]);
    // The server reads a wildcard in an issued grant as "every resource", so
    // the key must carry the chosen project rather than the candidate's
    // wildcards — otherwise it reads every project in the organization.
    expect(buildRequestedGrants(selections)).toEqual(required);
  });

  it("accepts an exact project candidate unchanged", () => {
    const { selections, missingScopes } = selectDeviceAgentKeyGrants(
      required,
      required,
    );
    expect(missingScopes).toEqual([]);
    expect(buildRequestedGrants(selections)).toEqual(required);
  });

  it("rejects a candidate that constrains an extra dimension", () => {
    const narrowed: AgentPolicyGrantForm = {
      effect: "allow",
      scope: "project:read",
      selector: {
        resourceKind: "project",
        resourceId: "*",
        tool: "only_this_tool",
      },
    };
    const { missingScopes } = selectDeviceAgentKeyGrants(
      [required[0]!, required[1]!, narrowed],
      required,
    );
    expect(missingScopes).toEqual(["project:read"]);
  });

  it("reports scopes no candidate covers", () => {
    const { missingScopes } = selectDeviceAgentKeyGrants(
      [required[2]!],
      required,
    );
    expect(missingScopes).toEqual([
      "org:device_agent_sync",
      "org:hooks_ingest",
    ]);
  });
});

describe("undelegable scopes message", () => {
  it("names org:admin when an org scope is missing", () => {
    expect(
      undelegableScopesMessage(["org:device_agent_sync", "project:read"]),
    ).toContain("org:admin");
  });

  it("points at policy and permissions when only project:read is missing", () => {
    const message = undelegableScopesMessage(["project:read"]);
    expect(message).toContain("project:read");
    expect(message).not.toContain("org:admin");
    expect(message).toContain("agent's policy");
  });
});

describe("agent purpose", () => {
  const connect = stored(
    {
      effect: "allow",
      scope: "mcp:connect",
      selector: { resourceKind: "mcp", resourceId: "*" },
    },
    "g1",
  );

  it("reads an agent that can sync as a device agent", () => {
    expect(agentPurposeFromPolicy([stored(required[0]!, "g2")])).toBe(
      "device-agent",
    );
  });

  it("reads any other agent as an MCP agent", () => {
    expect(agentPurposeFromPolicy([connect])).toBe("mcp");
    expect(agentPurposeFromPolicy([])).toBe("mcp");
  });

  it("detects a policy that reaches MCP servers", () => {
    expect(policyConnectsToMCP([connect])).toBe(true);
    expect(policyConnectsToMCP(required.map((g, i) => stored(g, `${i}`)))).toBe(
      false,
    );
  });
});

describe("device agent purpose gate", () => {
  it("is open to an org admin with the device agent enabled", () => {
    expect(
      deviceAgentPurposeBlocked({ deviceAgentEnabled: true, isOrgAdmin: true }),
    ).toBeNull();
  });

  it("names the rollout before the role", () => {
    expect(
      deviceAgentPurposeBlocked({
        deviceAgentEnabled: false,
        isOrgAdmin: false,
      }),
    ).toContain("not enabled");
  });

  it("requires org:admin", () => {
    expect(
      deviceAgentPurposeBlocked({
        deviceAgentEnabled: true,
        isOrgAdmin: false,
      }),
    ).toContain("org:admin");
  });
});

describe("review-first commands", () => {
  const commands = reviewCommands(
    "curl -fsSL https://gram.example.test/agent-mcp/install/code_1 | sh",
  );

  it("downloads to a fresh owner-only file, never an existing path", () => {
    expect(commands?.fetch).toBe(
      `f=$(mktemp ./gram-device-agent.XXXXXX) && if curl -fsSL 'https://gram.example.test/agent-mcp/install/code_1' -o "$f"; then echo "Saved to $f"; else rm -f "$f"; fi`,
    );
  });

  it("removes the file whether or not the run succeeds, keeping its status", () => {
    expect(commands?.run).toBe(`(sh "$f"; rc=$?; rm -f "$f"; exit $rc)`);
  });

  it("offers nothing for a plaintext install URL", () => {
    expect(
      reviewCommands(
        "curl -fsSL http://gram.example.test/agent-mcp/install/code_1 | sh",
      ),
    ).toBeNull();
  });

  it("offers nothing when the command holds no install URL", () => {
    expect(
      reviewCommands("curl -fsSL https://gram.example.test/other | sh"),
    ).toBeNull();
  });
});
