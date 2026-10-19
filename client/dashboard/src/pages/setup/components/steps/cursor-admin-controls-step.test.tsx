import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CursorAdminControlsStep } from "./cursor-admin-controls-step";

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
    matchesSource,
  }: {
    matchesSource: (source: string) => boolean;
  }) => (
    <p>
      Confirms cursor: {String(matchesSource("cursor"))}, claude-code:{" "}
      {String(matchesSource("claude-code"))}
    </p>
  ),
}));
vi.mock("../platform-setup-flow", () => ({
  PlatformSetupFlow: ({
    platformId,
    heldBack,
    orgHeldBack,
  }: {
    platformId: string;
    heldBack?: string;
    orgHeldBack?: string;
  }) => (
    <div>
      <p>{heldBack ?? `Steps for ${platformId}`}</p>
      {orgHeldBack ? <p>org rollout: {orgHeldBack}</p> : null}
    </div>
  ),
}));

afterEach(cleanup);

beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

describe("CursorAdminControlsStep", () => {
  it("walks the Cursor team marketplace and confirms only Cursor traffic", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      },
      isLoading: false,
    };

    render(<CursorAdminControlsStep onComplete={() => {}} />);

    expect(screen.getByText("Set up Cursor admin controls")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual(["Connect Cursor"]);
    expect(screen.getByText("Steps for cursor")).toBeTruthy();
    expect(
      screen.getByText("Confirms cursor: true, claude-code: false"),
    ).toBeTruthy();
  });

  it("holds the instructions until the marketplace is published", () => {
    render(<CursorAdminControlsStep onComplete={() => {}} />);

    expect(
      screen.getByText(/Publish the marketplace above first/),
    ).toBeTruthy();
  });

  it("holds the team import when the repo has no collaborators", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
        hasCollaborators: false,
      },
      isLoading: false,
    };

    render(<CursorAdminControlsStep onComplete={() => {}} />);

    expect(screen.getByText(/^org rollout: Add a collaborator/)).toBeTruthy();
  });
});
