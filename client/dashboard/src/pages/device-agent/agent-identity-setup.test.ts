import { describe, expect, it } from "vitest";

import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Key } from "@gram/client/models/components/key.js";

import { buildRequestedGrants } from "@/pages/agents/agent-api-key-grants";

import {
  buildAgentIdentityManagedConfig,
  buildAgentIdentitySnippet,
  deviceAgentPolicyGrants,
  lastSeenKey,
  missingPolicyGrants,
  selectDeviceAgentKeyGrants,
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
    os: "linux" as const,
    mode: "ephemeral" as const,
    serverURL: "https://app.getgram.ai",
  };

  it("carries no email, version, or checksum", () => {
    const snippet = buildAgentIdentitySnippet(base);
    expect(snippet).toContain(
      'curl -fsSL https://storage.googleapis.com/speakeasy-device-agent-releases-prod/install.sh | sh -s -- --install-dir "$BIN_DIR"',
    );
    expect(snippet).toContain("/etc/speakeasy/managed.json");
    expect(snippet).toContain('"$BIN_DIR/speakeasyd" sync --once');
    expect(snippet).not.toContain("loginctl");
    expect(snippet).not.toMatch(/email|sha256|VERSION/i);
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

  it("keeps the Linux service alive after logout", () => {
    const snippet = buildAgentIdentitySnippet({ ...base, mode: "service" });
    expect(snippet.indexOf('loginctl enable-linger "$USER"')).toBeLessThan(
      snippet.indexOf('"$BIN_DIR/speakeasyd" -service install'),
    );
    expect(snippet.indexOf("loginctl")).toBeGreaterThan(-1);
  });

  it("installs the service on persistent macOS hosts", () => {
    const snippet = buildAgentIdentitySnippet({
      ...base,
      os: "macos",
      mode: "service",
    });
    expect(snippet).toContain(
      "'/Library/Application Support/Speakeasy/managed.json'",
    );
    expect(snippet).toContain('"$BIN_DIR/speakeasyd" -service install');
    expect(snippet).not.toContain("sync --once");
    expect(snippet).not.toContain("loginctl");
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
