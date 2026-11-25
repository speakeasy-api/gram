import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { StepInstall } from "./StepInstall";

afterEach(cleanup);

const gatewayURL = "https://api.example.test/mcp/gateway/example/projects/main";

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
  // One plain command. The header is
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

it.each([
  [
    "project_assignment_required" as const,
    "This agent is not assigned to a project.",
    "Assign the agent to a project before setting up its runtime.",
  ],
  [
    "machine_access_disabled" as const,
    "Machine access to this project gateway is disabled.",
    "Ask an administrator to enable project gateway machine access, then reload this page.",
  ],
  [
    "turned_off" as const,
    "The project gateway is turned off.",
    "Turn on the project gateway, then reload this page.",
  ],
])(
  "withholds setup with an actionable %s state",
  (reason, description, action) => {
    render(
      <StepInstall
        onIssue={vi.fn<() => void>()}
        issuing={false}
        secret="gram_secret"
        existingKey={null}
        gatewayURL=""
        gatewayUnavailableReason={reason}
        serverCount={1}
      />,
    );

    expect(screen.getByText(description)).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain(action);
    expect(screen.queryByRole("tab", { name: "One-line setup" })).toBeNull();
    expect(screen.queryByLabelText("Claude Code")).toBeNull();
  },
);
