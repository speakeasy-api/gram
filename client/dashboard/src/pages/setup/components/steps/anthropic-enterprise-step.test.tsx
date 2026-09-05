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
  it("shows the published marketplace repo and opens the Cowork instructions", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-speakeasy",
        repoOwner: "acme",
        repoName: "acme-speakeasy",
      },
      isLoading: false,
    };

    render(<AnthropicEnterpriseStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByText("Set up Anthropic Enterprise")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "acme/acme-speakeasy" }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Claude Cowork/ }));

    expect(screen.getByText("Opened platform: claude-cowork")).toBeTruthy();
  });

  it("asks for the marketplace first when it is not published yet", () => {
    render(<AnthropicEnterpriseStep onComplete={() => {}} onBack={() => {}} />);

    expect(
      screen.getByText("Publish your plugin marketplace first"),
    ).toBeTruthy();
    const cowork = screen.getByRole("button", {
      name: /Claude Cowork/,
    }) as HTMLButtonElement;
    expect(cowork.disabled).toBe(true);
  });
});
