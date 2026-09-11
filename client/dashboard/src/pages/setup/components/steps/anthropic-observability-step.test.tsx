import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnthropicObservabilityStep } from "./anthropic-observability-step";

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

describe("AnthropicObservabilityStep", () => {
  it("shows both Anthropic providers and opens their shared setup sheet", () => {
    render(
      <AnthropicObservabilityStep onComplete={() => {}} onBack={() => {}} />,
    );

    expect(screen.getByText("Claude Code")).toBeTruthy();
    expect(screen.getByText("Claude Cowork")).toBeTruthy();

    fireEvent.click(screen.getByText("Claude Cowork"));
    expect(screen.getByText("Opened platform: claude-cowork")).toBeTruthy();
  });
});
