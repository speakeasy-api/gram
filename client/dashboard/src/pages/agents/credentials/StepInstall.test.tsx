import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { StepInstall } from "./StepInstall";

const AGENT_ID = "6f1c2a4e-0000-4000-8000-000000000001";
const KEY = "gram_live_example_key";
const CONFIG_LABEL = "2 · Save as managed.json — shown once";

function renderStep({
  secret = KEY,
  gatewayURL = `https://app.getgram.ai/agent-mcp/${AGENT_ID}`,
  onIssue = vi.fn<() => void>(),
}: {
  secret?: string | null;
  gatewayURL?: string;
  onIssue?: () => void;
} = {}) {
  const step = (stepSecret: string | null) => (
    <StepInstall
      command={null}
      commandError={null}
      minting={false}
      onRegenerate={vi.fn<() => void>()}
      onIssue={onIssue}
      issuing={false}
      secret={stepSecret}
      agentID={AGENT_ID}
      gatewayURL={gatewayURL}
      serverCount={2}
    />
  );
  const { rerender } = render(step(secret));
  return { removeKey: () => rerender(step(null)) };
}

function openDeviceAgentTab() {
  const tab = screen.getByRole("tab", { name: "Device Agent" });
  fireEvent.mouseDown(tab, { button: 0 });
  return tab;
}

/** A code block's text, once highlighting (which is async) has painted it. */
async function blockText(label: string): Promise<string> {
  const block = screen.getByLabelText(label);
  await waitFor(() => expect(block.textContent).not.toBe(""));
  return block.textContent ?? "";
}

async function managedConfig(): Promise<Record<string, unknown>> {
  return JSON.parse(await blockText(CONFIG_LABEL)) as Record<string, unknown>;
}

afterEach(cleanup);

describe("Device Agent tab", () => {
  it("renders the device agent setup steps", async () => {
    renderStep();
    const tab = openDeviceAgentTab();
    expect(tab.getAttribute("aria-selected")).toBe("true");
    expect(await blockText("1 · Install the device agent — Linux")).toContain(
      "https://storage.googleapis.com/speakeasy-device-agent-releases-prod/install.sh",
    );
    expect(await blockText("Move it to /etc/speakeasy/managed.json")).toContain(
      "-m 0640 managed.json /etc/speakeasy/managed.json",
    );
    expect(
      await blockText(
        "3 · Configure the tools — as the account that runs them",
      ),
    ).toBe("/opt/speakeasy/bin/speakeasy sync");
    expect(
      screen.getByText("~/.local/state/speakeasy/bin/speakeasy"),
    ).toBeTruthy();
    expect(screen.getByText(/Plugin sync and hooks are not/)).toBeTruthy();
    expect(
      screen.getByText(/written into those tools' MCP settings/),
    ).toBeTruthy();
  });

  it("carries the key and agent id, and no control plane on the default deployment", async () => {
    renderStep();
    openDeviceAgentTab();
    expect(await managedConfig()).toEqual({
      v: 1,
      mcp_credential: KEY,
      agent_id: AGENT_ID,
    });
  });

  it("names the control plane on any other deployment", async () => {
    renderStep({
      gatewayURL: `https://speakeasy.example.test/agent-mcp/${AGENT_ID}`,
    });
    openDeviceAgentTab();
    expect(await managedConfig()).toEqual({
      v: 1,
      mcp_credential: KEY,
      agent_id: AGENT_ID,
      _control_plane_url: "https://speakeasy.example.test",
    });
  });

  it("offers to issue a key instead of a config when none is available", async () => {
    const onIssue = vi.fn<() => void>();
    const { removeKey } = renderStep({ onIssue });
    openDeviceAgentTab();
    removeKey();
    const tab = screen.getByRole("tab", { name: "Device Agent", hidden: true });
    expect(tab.getAttribute("aria-selected")).toBe("true");
    expect((await managedConfig()).mcp_credential).not.toBe(KEY);
    fireEvent.click(screen.getByRole("button", { name: "Issue a key" }));
    expect(onIssue).toHaveBeenCalledOnce();
  });
});
