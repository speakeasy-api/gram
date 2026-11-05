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

function covered(existingKey?: { lastUsedAt?: Date } | null) {
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
    screen.getByRole("button", { name: "Issue another key" }),
  ).toBeTruthy();
});
