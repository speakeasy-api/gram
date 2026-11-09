import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { StepInstall } from "./StepInstall";

afterEach(cleanup);

const gatewayURL = "https://app.example.test/agent-mcp/agent_1";

it("adds the gateway to OpenClaw by hand with the key from the environment", () => {
  const writeText = vi.fn<(text: string) => Promise<void>>(() =>
    Promise.resolve(),
  );
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  render(
    <StepInstall
      command={null}
      commandError={null}
      minting={false}
      onRegenerate={vi.fn<() => void>()}
      onIssue={vi.fn<() => void>()}
      issuing={false}
      secret="gram_secret"
      existingKey={null}
      gatewayURL={gatewayURL}
      serverCount={1}
    />,
  );

  fireEvent.click(
    screen.getByRole("button", { name: "Copy OpenClaw 2026.10.6 or later" }),
  );

  const copied = writeText.mock.calls[0]?.[0] ?? "";
  // One plain command, named like the one-line setup's entry. The header is
  // single-quoted, so the shell never expands the key into argv: OpenClaw
  // saves the ${VAR} reference and resolves it from its environment.
  expect(copied).toContain(`openclaw mcp add speakeasy --url ${gatewayURL}`);
  expect(copied).toContain("--transport streamable-http --no-probe");
  expect(copied).toContain(
    "--header 'Authorization=Bearer ${SPEAKEASY_AGENT_KEY}'",
  );
  expect(copied).not.toContain('"Authorization=Bearer $SPEAKEASY_AGENT_KEY"');
  expect(copied).not.toContain("gram_secret");
});

function covered(existingKey: { lastUsedAt?: Date } | null | undefined) {
  render(
    <StepInstall
      command={null}
      commandError={null}
      minting={false}
      onRegenerate={vi.fn<() => void>()}
      onIssue={vi.fn<() => void>()}
      issuing={false}
      secret={null}
      existingKey={existingKey}
      gatewayURL={gatewayURL}
      serverCount={1}
    />,
  );
}

it("claims neither state while the agent's keys are unknown", () => {
  covered(undefined);
  expect(
    screen.getByText("A key that already exists cannot be shown here."),
  ).toBeTruthy();
  expect(
    screen.getByText(/any key the agent already holds keeps working/),
  ).toBeTruthy();
  expect(
    screen.queryByText("This agent needs a key before it can be connected."),
  ).toBeNull();
  expect(screen.getByRole("button", { name: "Issue a key" })).toBeTruthy();
});

it("asks for a key when the agent has none", () => {
  covered(null);
  expect(
    screen.getByText("This agent needs a key before it can be connected."),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: "Issue a key" })).toBeTruthy();
});

it("says an agent with a used key is already connected", () => {
  covered({ lastUsedAt: new Date(Date.now() - 3 * 3_600_000) });
  expect(
    screen.getByText(
      "This agent is already connected. Its key was last used 3h ago.",
    ),
  ).toBeTruthy();
  expect(screen.getByText(/the current one keeps working/)).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Issue another key" }),
  ).toBeTruthy();
});

it("says an agent already has a key that nothing has used yet", () => {
  covered({});
  expect(screen.getByText("This agent already has a key.")).toBeTruthy();
  expect(
    screen.getByText(/Nothing has used it yet: issue another only if/),
  ).toBeTruthy();
  expect(screen.queryByText(/the current one keeps working/)).toBeNull();
  expect(
    screen.getByRole("button", { name: "Issue another key" }),
  ).toBeTruthy();
});

it("tells an OpenClaw user how to update, reinstall or uninstall", () => {
  const writeText = vi.fn<(text: string) => Promise<void>>(() =>
    Promise.resolve(),
  );
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  render(
    <StepInstall
      command="curl -fsSL https://app.example.test/install/code | sh"
      commandError={null}
      minting={false}
      onRegenerate={vi.fn<() => void>()}
      onIssue={vi.fn<() => void>()}
      issuing={false}
      secret="gram_secret"
      existingKey={null}
      gatewayURL={gatewayURL}
      serverCount={1}
    />,
  );

  // Folded away on the one-line tab, where the setup command is.
  const summary = screen.getByText(
    "Need to update, reinstall or uninstall on OpenClaw?",
  );
  const disclosure = summary.closest("details");
  expect(disclosure?.open).toBe(false);
  fireEvent.click(summary);
  expect(disclosure?.open).toBe(true);
  const guidance = disclosure?.textContent ?? "";
  // Updating is running a new setup command, which needs a release that can
  // replace the gateway entry.
  expect(guidance).toContain(
    "does not update the Speakeasy hooks plugin on its own",
  );
  expect(guidance).toContain("update OpenClaw first");
  // The uninstall runs in a terminal; a container is restarted instead.
  expect(guidance).toContain("in a terminal where OpenClaw runs");
  expect(guidance).toContain("restart the");
  expect(guidance).toContain("container instead of running");
  fireEvent.click(
    screen.getByRole("button", { name: "Copy Uninstall from OpenClaw" }),
  );

  // The plugin id and the gateway entry name the one-line setup uses.
  expect(writeText.mock.calls[0]?.[0]).toBe(
    [
      "openclaw plugins uninstall speakeasy-observability --force",
      "openclaw config unset mcp.servers.speakeasy",
      "openclaw gateway restart",
    ].join("\n"),
  );
});
