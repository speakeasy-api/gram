import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AddExistingMCPServers,
  existingMCPServersPrompt,
} from "./add-existing-mcp-servers";

const mocks = vi.hoisted(() => ({
  organization: { id: "org-example" },
  query: vi.fn(),
  writeText: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
}));
vi.mock("@/hooks/useOrganizationPlatformMCPOnboarding", () => ({
  useOrganizationPlatformMCPOnboarding: mocks.query,
}));
const connected = {
  enabled: true,
  clientFamily: "claude_code",
  connectionAuthorized: true,
  connectionReady: true,
  reauthorizationReason: "",
};
beforeEach(() => {
  mocks.organization.id = "org-example";
  mocks.query.mockReset().mockReturnValue({ data: connected, isError: false });
  mocks.writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: mocks.writeText },
  });
});
afterEach(cleanup);

describe("AddExistingMCPServers", () => {
  it("preserves a visible keyboard-focus ring on the copy control", () => {
    render(<AddExistingMCPServers />);
    const button = screen.getByRole("button", { name: "Copy prompt" });
    button.focus();
    expect(document.activeElement).toBe(button);
    for (const utility of [
      "focus-visible:ring-2",
      "focus-visible:ring-offset-3",
      "focus-visible:ring-[var(--border-focus)]",
      "focus-visible:ring-offset-[var(--bg-surface-primary-default)]",
    ]) {
      expect(button.classList.contains(utility)).toBe(true);
    }
    expect(button.classList.contains("focus-visible:ring-0")).toBe(false);
  });
  it("offers an optional remote-only follow-up and installation guidance", () => {
    render(<AddExistingMCPServers currentProjectSlug="example-project" />);
    expect(
      screen.getByRole("region", {
        name: "Import existing MCP servers in Claude",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("heading", {
        name: "Import existing MCP servers in Claude",
      }),
    ).toBeTruthy();
    expect(screen.getByText("Optional")).toBeTruthy();
    expect(
      screen.getByText(
        "Already use remote MCP servers in Claude Code? Copy this prompt into Claude Code to check its Speakeasy connection before adding servers. This dashboard cannot verify which client is authenticated.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Review the per-server results in Claude Code. You can skip this step.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", {
          name: "Install the Speakeasy Platform MCP plugin",
        })
        .getAttribute("href"),
    ).toBe("https://github.com/speakeasy-api/marketplace#readme");
    expect(mocks.query).toHaveBeenCalledWith("org-example", {
      throwOnError: false,
      staleTime: 10_000,
    });
  });
  it.each([
    { name: "loading", data: undefined, isError: false },
    {
      name: "error with cached connected data",
      data: connected,
      isError: true,
    },
    ...[
      { name: "disabled", overrides: { enabled: false } },
      ...[
        "claude_cowork",
        "codex",
        "cursor",
        "opencode",
        "other",
        "unknown",
      ].map((clientFamily) => ({
        name: clientFamily,
        overrides: { clientFamily },
      })),
    ].map(({ name, overrides }) => ({
      name,
      data: { ...connected, ...overrides },
      isError: false,
    })),
  ])("hides actionable guidance when $name", ({ data, isError }) => {
    mocks.query.mockReturnValue({ data, isError });
    const { container } = render(<AddExistingMCPServers />);
    expect(container.innerHTML).toBe("");
  });
  it.each([
    {
      connectionAuthorized: false,
      connectionReady: false,
      reauthorizationReason: "revoked",
    },
    {
      connectionAuthorized: true,
      connectionReady: true,
      reauthorizationReason: "",
    },
  ])(
    "offers only a connection-check handoff regardless of generic auth: %j",
    (evidence) => {
      mocks.query.mockReturnValue({
        data: { ...connected, ...evidence },
        isError: false,
      });
      render(<AddExistingMCPServers />);
      expect(
        screen.getByText(
          /This dashboard cannot verify which client is authenticated/,
        ),
      ).toBeTruthy();
      const prompt = screen.getByText(existingMCPServersPrompt()).textContent!;
      expect(prompt).toContain("through your OWN Speakeasy connection");
      expect(prompt).toContain("Before any local discovery");
      expect(prompt).toContain("otherwise stop for Speakeasy sign-in/setup");
    },
  );
  it("does not retain another organization's panel while its query loads", () => {
    mocks.query.mockImplementation((id: string) => ({
      data: id === "org-example" ? connected : undefined,
      isError: false,
    }));
    const { rerender } = render(<AddExistingMCPServers />);
    expect(screen.getByRole("region")).toBeTruthy();
    mocks.organization.id = "org-other";
    rerender(<AddExistingMCPServers />);
    expect(screen.queryByRole("region")).toBeNull();
    expect(mocks.query).toHaveBeenLastCalledWith("org-other", {
      throwOnError: false,
      staleTime: 10_000,
    });
  });
  it("copies the exact displayed prompt without claiming import success", async () => {
    render(<AddExistingMCPServers currentProjectSlug="example-project" />);
    const prompt = existingMCPServersPrompt("example-project");
    expect(screen.getByText(prompt)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    expect(mocks.writeText).toHaveBeenCalledExactlyOnceWith(prompt);
    expect(await screen.findByRole("status")).toHaveProperty(
      "textContent",
      "Prompt copied",
    );
    expect(screen.queryByText("Mark done")).toBeNull();
  });
  it("resets copy feedback and copies the new destination after a project change", async () => {
    const { rerender } = render(
      <AddExistingMCPServers currentProjectSlug="example-project" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    await screen.findByRole("status");
    rerender(<AddExistingMCPServers currentProjectSlug="other-project" />);
    expect(screen.queryByRole("status")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    expect(mocks.writeText).toHaveBeenLastCalledWith(
      existingMCPServersPrompt("other-project"),
    );
    await screen.findByRole("status");
  });
  it("resets copy feedback when switching between connected organizations", async () => {
    const { rerender } = render(<AddExistingMCPServers />);
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    await screen.findByRole("status");
    mocks.organization.id = "org-other";
    rerender(<AddExistingMCPServers />);
    expect(screen.queryByRole("status")).toBeNull();
  });
  it("handles clipboard failure without claiming it copied", async () => {
    mocks.writeText.mockRejectedValue(new Error("Permission denied"));
    render(<AddExistingMCPServers />);
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Could not copy the prompt. Select and copy it manually.",
    );
    expect(screen.queryByText("Prompt copied")).toBeNull();
  });
});
describe("existingMCPServersPrompt", () => {
  it("preserves the exact destination as quoted data and asks confirmation", () => {
    const project = 'example-"quoted"-project';
    expect(existingMCPServersPrompt(project)).toContain(
      `Ask me to confirm the destination project ${JSON.stringify(project)}.`,
    );
  });
  it("asks for project selection when no project is available", () => {
    expect(existingMCPServersPrompt()).toContain(
      "List eligible Speakeasy projects and ask me to choose the destination.",
    );
    expect(existingMCPServersPrompt()).not.toContain("undefined");
  });
  it("keeps the complete consent, skill-installation, credential and verification boundaries", () => {
    expect(existingMCPServersPrompt()).toBe(
      [
        "Help me add remote MCP servers I already use in Claude Code to Speakeasy.",
        "Use the add-existing-mcp-servers skill if available. If it is unavailable, stop and guide me to install the Speakeasy Platform MCP plugin before proceeding.",
        "Before any local discovery, successfully call list_projects through your OWN Speakeasy connection in this Claude Code session; otherwise stop for Speakeasy sign-in/setup. Dashboard state or another client's authentication is not proof of access.",
        "Before running claude mcp list, explain that it health-checks approved servers, can launch stdio processes and contact local/private-network endpoints BEFORE filtering, and may cause process side effects. Obtain explicit informed consent for those effects, or offer a user-sanitized manual inventory instead without running discovery. Show only sanitized inventory and explain excluded servers.",
        "List eligible Speakeasy projects and ask me to choose the destination.",
        "Ask which servers I want, check for existing registrations, inspect missing candidates, and confirm the exact batch before adding anything.",
        "For each missing server, search the catalogue by exact endpoint first, then by provider/name as a secondary lookup. Show candidate differences (endpoint, provider, capabilities, and authentication) and require my confirmation before substituting a catalogue candidate. Register confirmed candidates through the catalogue registration flow, not as direct remote servers. If I decline a candidate or no match exists, use the original URL registration path.",
        "Never copy local credentials or change local configuration. Authentication must happen through Speakeasy's secure setup flow.",
        "Verify and report every selected server separately. Do not claim partial success is complete, or require plugin/gateway distribution.",
      ].join(" "),
    );
  });
});
