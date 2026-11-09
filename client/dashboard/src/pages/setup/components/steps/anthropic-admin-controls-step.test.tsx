import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AnthropicAdminControlsStep } from "./anthropic-admin-controls-step";

const keyMocks = vi.hoisted(() => ({
  usePlatformApiKeys: vi.fn(() => ({
    keys: {},
    pending: {},
    errors: {},
    ensure: vi.fn(),
  })),
  flow: vi.fn(),
  stepList: vi.fn(),
}));
vi.mock("../platform-setup-values", () => ({
  usePlatformApiKeys: keyMocks.usePlatformApiKeys,
}));

const publishStatus = vi.hoisted(() => ({
  current: {
    data: { connected: false } as Record<string, unknown>,
    isLoading: false,
  },
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
}));
vi.mock("../enable-logging-section", () => ({
  EnableLoggingSection: () => <div>Enable logging section</div>,
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: ({
    requiresCollaborators,
  }: {
    requiresCollaborators?: boolean;
  }) => (
    <div>
      Marketplace section
      {requiresCollaborators ? " (needs a collaborator)" : ""}
    </div>
  ),
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: ({
    description,
    callout,
  }: {
    description: string;
    callout?: { title: string; body: string };
  }) => (
    <div>
      <p>Confirm traffic section: {description}</p>
      <p>{callout?.title}</p>
      <p>{callout?.body}</p>
    </div>
  ),
}));
vi.mock("../organization-gateway-section", () => ({
  OrganizationGatewayConnectorSection: () => (
    <div>Organization Gateway connector section</div>
  ),
}));
vi.mock("../platform-setup-flow", () => ({
  PlatformSetupFlow: ({
    platformId,
    heldBack,
    onStatusChange,
    apiKeys,
  }: {
    apiKeys?: unknown;
    platformId: string;
    heldBack?: string;
    onStatusChange: (status: string) => void;
  }) => {
    keyMocks.flow(platformId, apiKeys);
    return (
      <div>
        <p>{heldBack ?? `Steps for ${platformId}`}</p>
        <button onClick={() => onStatusChange("complete")}>
          Connect {platformId}
        </button>
      </div>
    );
  },
}));
vi.mock("../platform-step-list", () => ({
  PlatformStepList: ({
    platformId,
    steps,
    heldBack,
    apiKeys,
  }: {
    platformId: string;
    steps: Array<{ title: string }>;
    heldBack?: string;
    apiKeys?: unknown;
  }) => {
    keyMocks.stepList(platformId, apiKeys);
    return (
      <p>
        {heldBack ??
          `${platformId} steps: ${steps.map(({ title }) => title).join(" | ")}`}
      </p>
    );
  },
}));

afterEach(cleanup);

beforeEach(() => {
  keyMocks.flow.mockClear();
  keyMocks.stepList.mockClear();
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

function renderStep() {
  return render(<AnthropicAdminControlsStep onComplete={() => {}} />);
}

const published = {
  connected: true,
  repoUrl: "https://github.com/acme/acme-plugins",
  marketplaceUrl: "https://app.example.com/marketplace/tok.git",
};

describe("AnthropicAdminControlsStep", () => {
  it("covers Claude Code and Cowork monitoring, and leaves plugin registration to Set up Claude Desktop", () => {
    publishStatus.current = { data: published, isLoading: false };

    renderStep();

    expect(keyMocks.usePlatformApiKeys).toHaveBeenCalledWith({
      shareAnthropicKey: true,
    });
    const [, claudeKeys] = keyMocks.flow.mock.calls.find(
      ([id]) => id === "claude",
    )!;
    const [, coworkKeys] = keyMocks.stepList.mock.calls.find(
      ([id]) => id === "claude-cowork",
    )!;
    expect(claudeKeys).toBeDefined();
    expect(coworkKeys).toBe(claudeKeys);
    expect(
      screen.getByText(/One Hooks API key is generated automatically/),
    ).toBeTruthy();
    expect(screen.getByText("Set up Anthropic admin controls")).toBeTruthy();
    expect(
      screen.getByText(/have their own cards under Set up Claude Desktop/),
    ).toBeTruthy();
    expect(screen.getByText("Enable logging section")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(screen.getAllByText("Optional")).toHaveLength(1);
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual([
      "Connect Claude Code",
      "Turn on Cowork monitoring",
      "Add the Organization Gateway to Claude",
    ]);

    expect(screen.getByText("Steps for claude")).toBeTruthy();
    expect(
      screen.getByText("claude-cowork steps: Enable OTEL export"),
    ).toBeTruthy();
    expect(screen.queryByText(/Sync from GitHub/)).toBeNull();
    expect(
      keyMocks.flow.mock.calls.some(([id]) => id === "claude-cowork"),
    ).toBe(false);
    expect(screen.queryByText(/Cursor/)).toBeNull();
  });

  it("sends Cowork checks to Agent Sessions instead of the traffic list", () => {
    publishStatus.current = { data: published, isLoading: false };

    renderStep();

    expect(screen.getByText("Check Cowork in Agent Sessions")).toBeTruthy();
    expect(
      screen.getByText(/Cowork conversations don't appear in this list/),
    ).toBeTruthy();
    expect(
      screen.getByText(/look for it in Agent Sessions after the first reply/),
    ).toBeTruthy();
  });

  it("tracks Claude Code's connected badge", () => {
    publishStatus.current = { data: published, isLoading: false };

    renderStep();

    expect(screen.queryByText("Complete")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Connect claude" }));

    expect(screen.getAllByText("Complete")).toHaveLength(1);
  });

  it("doesn't wait on a GitHub collaborator, which only Claude.ai's plugin sync needs", () => {
    // Claude Code clones through Speakeasy's proxy. Registering the plugins in
    // Claude.ai, which syncs through GitHub, is its own card.
    publishStatus.current = {
      data: { ...published, hasCollaborators: false },
      isLoading: false,
    };

    renderStep();

    expect(screen.queryByText(/Add a collaborator/)).toBeNull();
    expect(screen.queryByText(/needs a collaborator/)).toBeNull();
    expect(screen.getByText("Steps for claude")).toBeTruthy();
  });

  it("holds Claude Code's instructions until the marketplace is published", () => {
    renderStep();

    expect(
      screen.getAllByText(/Publish the marketplace above first/),
    ).toHaveLength(1);
    // Native monitoring doesn't read the marketplace.
    expect(
      screen.getByText("claude-cowork steps: Enable OTEL export"),
    ).toBeTruthy();
  });

  it("still holds them when a connection has no marketplace URL yet", () => {
    // A GitHub connection written before marketplace tokens were minted
    // reports connected with a repo and no marketplace URL; the snippets all
    // interpolate that URL, so the card must keep prompting for a publish.
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-plugins",
      },
      isLoading: false,
    };

    renderStep();

    expect(
      screen.getAllByText(/Publish the marketplace above first/),
    ).toHaveLength(1);
  });
});
