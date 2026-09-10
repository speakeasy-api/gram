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
vi.mock("../enable-logging-section", () => ({
  EnableLoggingSection: () => <div>Enable logging section</div>,
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: () => <div>Marketplace section</div>,
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
  it("gives Cowork, Claude Code and Cursor a section of their own, Cowork first", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-plugins",
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      },
      isLoading: false,
    };

    renderStep();

    expect(screen.getByText("Set up Anthropic observability")).toBeTruthy();
    expect(screen.getByText("Enable logging section")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(screen.getByText("Optional")).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual([
      "Connect Claude Cowork",
      "Connect Claude Code",
      "Connect Cursor",
    ]);

    expect(screen.getByText("Steps for claude")).toBeTruthy();
    expect(screen.getByText("Steps for claude-cowork")).toBeTruthy();
    expect(screen.getByText("Steps for cursor")).toBeTruthy();
  });

  it("calls out that Cowork reports nothing until its toggle is on", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      },
      isLoading: false,
    };

    renderStep();

    expect(screen.getByText("Turn Cowork on before you chat")).toBeTruthy();
    expect(
      screen.getByText(
        /switch the Claude Cowork toggle on, then send a message/,
      ),
    ).toBeTruthy();
  });

  it("tracks each platform's connected badge on its own", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      },
      isLoading: false,
    };

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
    ).toHaveLength(3);
  });
});
