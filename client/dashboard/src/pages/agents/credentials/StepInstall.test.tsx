import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { StepInstall } from "./StepInstall";

afterEach(cleanup);

const gatewayURL = "https://app.example.test/agent-mcp/agent_1";

it("adds the gateway to OpenClaw by hand without the key in argv", () => {
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
  // OpenClaw sends headers as written, so the key goes into its config
  // through a private batch file the shell fills in, never through argv.
  expect(copied).toContain("umask 077");
  expect(copied).toContain('openclaw config set --batch-file "$batch"');
  // A failed update fails the block and skips the chmod.
  expect(copied).toContain('exit "$rc"');
  expect(copied).toContain(") && chmod 600 ~/.openclaw/openclaw.json");
  expect(copied).not.toContain("openclaw mcp add");
  const batch = copied.split("\n").find((line) => line.startsWith("[{"));
  expect(JSON.parse(batch ?? "")).toEqual([
    {
      path: "mcp.servers.speakeasy",
      value: {
        url: gatewayURL,
        transport: "streamable-http",
        headers: { Authorization: "Bearer $SPEAKEASY_AGENT_KEY" },
      },
    },
  ]);
  expect(copied).not.toContain("gram_secret");
});
