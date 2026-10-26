import { describe, expect, it } from "vitest";

import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

import { buildRequestedGrants } from "../agent-api-key-grants";

import {
  deviceAgentPolicyGrants,
  selectDeviceAgentKeyGrants,
  undelegableScopesMessage,
  reviewCommands,
} from "./device-agent";

const PROJECT = "proj-1";
const required = deviceAgentPolicyGrants(PROJECT);

describe("device agent policy grants", () => {
  it("requires sync and hooks on the org and read on the project", () => {
    expect(required.map((g) => [g.scope, g.selector])).toEqual([
      ["org:device_agent_sync", { resourceKind: "org", resourceId: "*" }],
      ["org:hooks_ingest", { resourceKind: "org", resourceId: "*" }],
      ["project:read", { resourceKind: "project", resourceId: PROJECT }],
    ]);
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

describe("review-first commands", () => {
  const commands = reviewCommands(
    "curl -fsSL https://gram.example.test/agent-mcp/install/code_1 | sh",
  );

  it("downloads to a fresh owner-only file, never an existing path", () => {
    expect(commands?.fetch).toBe(
      `f=$(mktemp ./gram-device-agent.XXXXXX) && if curl -fsSL 'https://gram.example.test/agent-mcp/install/code_1' -o "$f"; then echo "Saved to $f"; else rc=$?; rm -f "$f"; (exit "$rc"); fi`,
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
