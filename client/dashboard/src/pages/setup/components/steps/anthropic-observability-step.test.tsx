import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AnthropicObservabilityStep } from "./anthropic-observability-step";

const publishStatus = vi.hoisted(() => ({
  current: {
    data: { connected: false } as Record<string, unknown>,
    isLoading: false,
  },
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: () => <div>Marketplace section</div>,
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: () => <div>Confirm traffic section</div>,
}));
vi.mock("../platform-setup-flow", () => ({
  PlatformSetupFlow: ({
    platformId,
    heldBack,
    onStatusChange,
  }: {
    platformId: string;
    heldBack?: string;
    onStatusChange: (status: string) => void;
  }) => (
    <div>
      <p>{heldBack ?? `Steps for ${platformId}`}</p>
      <button onClick={() => onStatusChange("complete")}>
        Connect {platformId}
      </button>
    </div>
  ),
}));

afterEach(cleanup);

beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

function renderStep() {
  return render(<AnthropicObservabilityStep onComplete={() => {}} />);
}

describe("AnthropicObservabilityStep", () => {
  it("gives Claude Code, Chat and Cowork, and Cursor a section of their own", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-plugins",
      },
      isLoading: false,
    };

    renderStep();

    expect(screen.getByText("Set up Anthropic observability")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(screen.getByText("Connect Claude Code")).toBeTruthy();
    expect(screen.getByText("Connect Claude Chat and Cowork")).toBeTruthy();
    expect(screen.getByText("Connect Cursor")).toBeTruthy();
    expect(screen.getByText("Optional")).toBeTruthy();
    expect(screen.getByText("Confirm traffic section")).toBeTruthy();

    expect(screen.getByText("Steps for claude")).toBeTruthy();
    expect(screen.getByText("Steps for claude-cowork")).toBeTruthy();
    expect(screen.getByText("Steps for cursor")).toBeTruthy();
  });

  it("tracks each platform's connected badge on its own", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };

    renderStep();

    expect(screen.queryByText("Complete")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Connect claude" }));

    expect(screen.getAllByText("Complete")).toHaveLength(1);
  });

  it("holds every set of instructions until the marketplace is published", () => {
    renderStep();

    expect(
      screen.getAllByText(/Publish the marketplace above first/),
    ).toHaveLength(3);
  });
});
