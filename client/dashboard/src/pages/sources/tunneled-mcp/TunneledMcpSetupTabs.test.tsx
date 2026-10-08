import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  tunnelGatewayURL: () => "wss://tunnel.speakeasy.com/connect",
}));

// Renders the snippet with each slot's copy text in place, as copied.
vi.mock("@/components/code", () => ({
  CodeBlock: ({
    children,
    slots,
  }: {
    children: string;
    slots?: Record<string, { copyText: string }>;
  }) => {
    let text = children;
    for (const [sentinel, slot] of Object.entries(slots ?? {})) {
      text = text.split(sentinel).join(slot.copyText);
    }
    return <pre data-testid="snippet">{text}</pre>;
  },
}));

import { TunneledMcpSetupTabs } from "./TunneledMcpSetupTabs";

afterEach(() => {
  cleanup();
});

function choose(name: string) {
  fireEvent.mouseDown(screen.getByRole("tab", { name }), { button: 0 });
}

function snippet(): string {
  return screen.getByTestId("snippet").textContent ?? "";
}

describe("TunneledMcpSetupTabs per-user credentials", () => {
  it("leaves the stdio snippet unchanged in shared mode", () => {
    render(<TunneledMcpSetupTabs serverName="okta" />);
    choose("Stdio");
    expect(snippet()).not.toContain("TUNNEL_STDIO_CREDENTIALS");
    expect(snippet()).not.toContain("TUNNEL_IDENTITY_");
  });

  it("adds verifier settings and a memory-backed volume per user", () => {
    render(
      <TunneledMcpSetupTabs
        serverName="okta"
        tunneledMcpServerId="00000000-0000-4000-8000-000000000001"
      />,
    );
    choose("Stdio");
    choose("Per user");

    const kubernetes = snippet();
    expect(kubernetes).toContain("name: TUNNEL_STDIO_CREDENTIALS");
    expect(kubernetes).toContain(
      'value: "tunneled-mcp-server:00000000-0000-4000-8000-000000000001"',
    );
    expect(kubernetes).toContain('value: "org_example"');
    expect(kubernetes).toContain("medium: Memory");
    expect(kubernetes).toContain("mountPath: /dev/shm");
    // runAsNonRoot needs a numeric user, matching the image's USER.
    expect(kubernetes).toContain("runAsUser: 1000");

    choose("Docker");
    const docker = snippet();
    expect(docker).toContain(
      "-e TUNNEL_IDENTITY_ORGANIZATION_ID='org_example'",
    );
    expect(docker).toContain("exec /opt/mcp/bin/your-mcp-server");
    expect(docker).toContain("ENV TUNNEL_STDIO_CREDENTIALS=user");
    expect(docker).toContain('"/sbin/tini", "--"');
    expect(docker).toContain("USER 1000:1000");
    expect(docker).toContain(
      "-e TUNNEL_IDENTITY_ISSUER='https://tunnel.speakeasy.com'",
    );
  });

  it("uses the saved resource identifier as the audience", () => {
    render(
      <TunneledMcpSetupTabs
        serverName="okta"
        tunneledMcpServerId="00000000-0000-4000-8000-000000000001"
        resourceIdentifier="https://mcp.internal.example.com/mcp"
      />,
    );
    choose("Stdio");
    choose("Per user");
    expect(snippet()).toContain(
      'value: "https://mcp.internal.example.com/mcp"',
    );
  });

  it("templates an edited issuer into the snippet", () => {
    render(<TunneledMcpSetupTabs serverName="okta" />);
    choose("Stdio");
    choose("Per user");
    fireEvent.change(screen.getByLabelText("Assertion issuer"), {
      target: { value: "https://tunnel.example.test" },
    });
    expect(snippet()).toContain('value: "https://tunnel.example.test"');
    expect(snippet()).toContain(
      'value: "tunneled-mcp-server:<TUNNELED_MCP_SERVER_ID>"',
    );
  });
});
