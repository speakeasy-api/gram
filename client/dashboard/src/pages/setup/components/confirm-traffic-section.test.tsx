import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfirmTrafficSection } from "./confirm-traffic-section";
import {
  isAnthropicOrCursorSource,
  isOtherPlatformSource,
} from "./hook-event-sources";

const mocks = vi.hoisted(() => ({
  query: {
    data: undefined as
      | undefined
      | { events: Array<Record<string, unknown>>; latestUnixNano: string },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
  detections: { data: undefined as undefined | { detections: unknown[] } },
}));

vi.mock("@gram/client/react-query/verifyOnboardingHooksSetup.js", () => ({
  useVerifyOnboardingHooksSetup: () => mocks.query,
}));
vi.mock("@gram/client/react-query/aiDetections.js", () => ({
  useAiDetections: () => mocks.detections,
}));

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => children,
  motion: { div: "div" },
}));

function nowNano(): string {
  return String(BigInt(Date.now()) * 1_000_000n);
}

function poll(source: string) {
  mocks.query.data = {
    events: [{ source, timeUnixNano: nowNano(), eventName: "tool call" }],
    latestUnixNano: nowNano(),
  };
}

afterEach(cleanup);
beforeEach(() => {
  mocks.query.data = undefined;
  mocks.query.isLoading = false;
  mocks.query.isError = false;
  mocks.query.refetch.mockReset();
  mocks.detections.data = undefined;
});

describe("ConfirmTrafficSection", () => {
  it("flips from waiting to confirmed once a matching event arrives", () => {
    const view = render(
      <ConfirmTrafficSection
        index={3}
        description="Run a tool."
        matchesSource={isOtherPlatformSource}
      />,
    );
    expect(screen.getByText("Waiting")).toBeTruthy();

    poll("codex");
    view.rerender(
      <ConfirmTrafficSection
        index={3}
        description="Run a tool."
        matchesSource={isOtherPlatformSource}
      />,
    );

    expect(screen.getByText("Confirmed")).toBeTruthy();
  });

  it("ignores events from sources the card is not about", () => {
    const view = render(
      <ConfirmTrafficSection
        index={3}
        description="Start a Cowork session."
        matchesSource={isAnthropicOrCursorSource}
      />,
    );

    poll("codex");
    view.rerender(
      <ConfirmTrafficSection
        index={3}
        description="Start a Cowork session."
        matchesSource={isAnthropicOrCursorSource}
      />,
    );

    expect(screen.getByText("Waiting")).toBeTruthy();
    expect(screen.queryByText("Confirmed")).toBeNull();
  });

  it("shows polling failures and retries them", () => {
    mocks.query.isError = true;
    render(<ConfirmTrafficSection index={3} description="Run a tool." />);

    expect(screen.getByRole("alert").textContent).toContain(
      "We couldn't check for traffic. Try again.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });
});

describe("ConfirmTrafficSection detected clients", () => {
  function detection(targetId: string, displayName: string) {
    return { targetId, displayName, category: "harness" };
  }

  it("names the clients the agent found, filtered to this card", () => {
    mocks.detections.data = {
      detections: [
        detection("claude-code", "Claude Code"),
        detection("cursor", "Cursor"),
        detection("codex", "Codex"),
      ],
    };

    render(
      <ConfirmTrafficSection
        index={3}
        description="Run a tool."
        matchesSource={isAnthropicOrCursorSource}
      />,
    );

    expect(screen.getByText("Open one of these clients:")).toBeTruthy();
    expect(screen.getByText("Claude Code")).toBeTruthy();
    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.queryByText("Codex")).toBeNull();
  });

  it("keeps the generic prompt when nothing has been detected", () => {
    render(<ConfirmTrafficSection index={3} description="Run a tool." />);

    expect(screen.queryByText("Open one of these clients:")).toBeNull();
  });
});
