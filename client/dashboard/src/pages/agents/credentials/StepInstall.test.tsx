import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { StepInstall } from "./StepInstall";

afterEach(cleanup);

const gatewayURL = "https://app.example.test/agent-mcp/agent_1";

it("adds the gateway to OpenClaw by hand with the key the shell expands", () => {
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

  fireEvent.click(screen.getByRole("button", { name: "Copy OpenClaw" }));

  const copied = writeText.mock.calls[0]?.[0] ?? "";
  expect(copied).toContain(`openclaw mcp add speakeasy --url ${gatewayURL}`);
  expect(copied).toContain("--transport streamable-http");
  // OpenClaw sends headers as written, so the block names the variable for
  // the shell to expand and never carries the key.
  expect(copied).toContain(
    '--header "Authorization=Bearer $SPEAKEASY_AGENT_KEY"',
  );
  expect(copied).not.toContain("gram_secret");
});
