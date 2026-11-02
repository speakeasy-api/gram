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
