import { describe, expect, it } from "vitest";

import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Key } from "@gram/client/models/components/key.js";

import { buildRequestedGrants } from "@/pages/agents/agent-api-key-grants";

import {
  buildAgentIdentityManagedConfig,
  buildAgentIdentitySnippet,
  controlPlaneURLError,
  deviceAgentPolicyGrants,
  issuedKeyFor,
  lastSeenKey,
  missingPolicyGrants,
  selectDeviceAgentKeyGrants,
  undelegableScopesMessage,
} from "./agent-identity-setup";

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

describe("agent identity snippet", () => {
  const base = {
    agentKey: "gram_live_abc123",
    mode: "ephemeral" as const,
    serverURL: "https://app.getgram.ai",
  };

  it("carries no email, version, or checksum", () => {
    const snippet = buildAgentIdentitySnippet(base);
    expect(snippet).toContain("/etc/speakeasy/managed.json");
    expect(snippet).toContain('"$BIN_DIR/speakeasyd" sync --once');
    expect(snippet).not.toContain("loginctl");
    expect(snippet).not.toMatch(/email|sha256|VERSION/i);
  });

  it("downloads the installer over HTTPS only before running it", () => {
    const snippet = buildAgentIdentitySnippet(base);
    expect(snippet).toContain(
      "curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2",
    );
    expect(snippet).toContain(
      '-o "$INSTALLER" https://storage.googleapis.com/speakeasy-device-agent-releases-prod/install.sh',
    );
    expect(snippet).toContain("trap 'rm -f \"$INSTALLER\"' EXIT");
    expect(snippet).toContain('sh "$INSTALLER" --install-dir "$BIN_DIR"');
    expect(snippet).not.toMatch(/\|\s*sh/);
    expect(snippet.indexOf("curl -fsSL")).toBeLessThan(
      snippet.indexOf('sh "$INSTALLER"'),
    );
  });

  it("writes the config to the Linux managed path in both modes", () => {
    for (const mode of ["ephemeral", "service"] as const) {
      const snippet = buildAgentIdentitySnippet({ ...base, mode });
      expect(snippet).toContain("$SUDO mkdir -p '/etc/speakeasy'");
      expect(snippet).toContain("$SUDO tee '/etc/speakeasy/managed.json'");
    }
  });

  it("writes an agent-key config and omits the production control plane", () => {
    expect(JSON.parse(buildAgentIdentityManagedConfig(base))).toEqual({
      v: 1,
      agent_key: "gram_live_abc123",
      environment: "ephemeral",
      hide_ui: true,
      auto_update: "disabled",
    });
  });

  it("points non-production servers at their control plane", () => {
    const config = JSON.parse(
      buildAgentIdentityManagedConfig({
        ...base,
        mode: "service",
        serverURL: "https://dev.getgram.ai/",
      }),
    );
    expect(config._control_plane_url).toBe("https://dev.getgram.ai");
    expect(config.environment).toBe("server");
    expect(config.auto_update).toBe("automatic");
  });

  it("refuses a plaintext control plane", () => {
    expect(() =>
      buildAgentIdentitySnippet({
        ...base,
        serverURL: "http://gram.example.com",
      }),
    ).toThrow(/HTTPS/);
  });

  it("keeps the service alive after logout", () => {
    const snippet = buildAgentIdentitySnippet({ ...base, mode: "service" });
    const linger =
      'if [ -n "$SUDO" ]; then\n  sudo loginctl enable-linger "$(id -un)"\nfi';
    expect(snippet).toContain(linger);
    expect(snippet.indexOf(linger)).toBeLessThan(
      snippet.indexOf('"$BIN_DIR/speakeasyd" -service install'),
    );
    expect(snippet).toContain('"$BIN_DIR/speakeasyd" -service start');
    expect(snippet).not.toContain("sync --once");
    // Root installs skip linger, and $USER may be unset in containers.
    expect(snippet).not.toContain("$USER");
  });

  it("rejects a key that would break the JSON", () => {
    expect(() =>
      buildAgentIdentitySnippet({ ...base, agentKey: 'a"b' }),
    ).toThrow();
    expect(() =>
      buildAgentIdentitySnippet({ ...base, agentKey: "" }),
    ).toThrow();
  });
});

describe("control plane URL", () => {
  it("accepts HTTPS", () => {
    expect(controlPlaneURLError("https://app.getgram.ai")).toBeNull();
    expect(controlPlaneURLError("https://dev.getgram.ai/")).toBeNull();
  });

  it("rejects plain HTTP even on loopback", () => {
    expect(controlPlaneURLError("http://localhost:8080")).toMatch(/HTTPS/);
    expect(controlPlaneURLError("http://127.0.0.1:8080")).toMatch(/HTTPS/);
  });

  it("rejects plaintext remote hosts and invalid URLs", () => {
    expect(controlPlaneURLError("http://gram.example.com")).toMatch(/HTTPS/);
    expect(controlPlaneURLError("http://10.0.0.5")).toMatch(/HTTPS/);
    expect(controlPlaneURLError("not a url")).toMatch(/invalid/);
  });
});

describe("agent identity config permissions", () => {
  it.each(["ephemeral", "service"] as const)(
    "limits the %s config to root and the agent's account",
    (mode) => {
      const snippet = buildAgentIdentitySnippet({
        agentKey: "gram_live_abc123",
        mode,
        serverURL: "https://app.getgram.ai",
      });
      expect(snippet).toContain("chmod 0600 '/etc/speakeasy/managed.json'");
      expect(snippet).toContain(
        `sudo chown root:"$(id -gn)" '/etc/speakeasy/managed.json'`,
      );
      expect(snippet).toContain(
        "sudo chmod 0640 '/etc/speakeasy/managed.json'",
      );
      expect(snippet).not.toContain("0644");
    },
  );
});

describe("issued key binding", () => {
  const issued = { agentId: "a1", projectId: "p1", keyId: "k1", value: "v" };

  it("keeps the key for the agent and project it was minted for", () => {
    expect(issuedKeyFor(issued, "a1", "p1")).toBe(issued);
  });

  it("drops the key when the project or agent changes", () => {
    expect(issuedKeyFor(issued, "a1", "p2")).toBeNull();
    expect(issuedKeyFor(issued, "a2", "p1")).toBeNull();
    expect(issuedKeyFor(issued, undefined, "p1")).toBeNull();
    expect(issuedKeyFor(null, "a1", "p1")).toBeNull();
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

describe("last seen", () => {
  it("picks the most recently used key", () => {
    const keys = [
      { id: "a", lastAccessedAt: new Date("2026-09-01") },
      { id: "b" },
      { id: "c", lastAccessedAt: new Date("2026-09-10") },
    ] as Key[];
    expect(lastSeenKey(keys)?.id).toBe("c");
    expect(lastSeenKey([{ id: "b" } as Key])).toBeNull();
  });
});
