import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AnthropicEnterpriseStep } from "./anthropic-enterprise-step";

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
vi.mock("../platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: ({
    open,
    initialPlatformId,
  }: {
    open: boolean;
    initialPlatformId?: string;
  }) => (open ? <div>Opened platform: {initialPlatformId}</div> : null),
}));

afterEach(cleanup);

beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

describe("AnthropicEnterpriseStep", () => {
  it("stacks marketplace, Cowork, and traffic sections in one card", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-plugins",
      },
      isLoading: false,
    };

    render(<AnthropicEnterpriseStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByText("Set up Anthropic Enterprise")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(screen.getByText("Confirm traffic section")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Claude Cowork/ }));

    expect(screen.getByText("Opened platform: claude-cowork")).toBeTruthy();
  });

  it("holds the Cowork instructions until the marketplace is published", () => {
    render(<AnthropicEnterpriseStep onComplete={() => {}} onBack={() => {}} />);

    const cowork = screen.getByRole("button", {
      name: /Claude Cowork/,
    }) as HTMLButtonElement;
    expect(cowork.disabled).toBe(true);
    expect(
      screen.getByText(/Publish the marketplace above first/),
    ).toBeTruthy();
  });
});
