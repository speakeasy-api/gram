import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ExecutionShareChart } from "./ExecutionShareChart";
import type { ModeShare } from "./gatewayActivity";
import { GatewayModeTiles } from "./GatewayModeTiles";

afterEach(cleanup);

function share(overrides: Partial<ModeShare>): ModeShare {
  return {
    mode: "progressive",
    label: "Progressive",
    discoveryCalls: 0,
    executionCalls: 0,
    executeRuns: 0,
    share: null,
    previousShare: null,
    ...overrides,
  };
}

describe("GatewayModeTiles", () => {
  it("shows each mode's share, its change in points, and the counts behind it", () => {
    render(
      <TooltipProvider>
        <GatewayModeTiles
          loading={false}
          modes={[
            share({
              discoveryCalls: 42,
              executionCalls: 78,
              share: 0.65,
              previousShare: 0.61,
            }),
            share({
              mode: "code_mode",
              label: "Code Mode",
              discoveryCalls: 1,
              executionCalls: 1,
              executeRuns: 1,
              share: 0.5,
              previousShare: 0.5,
            }),
            share({
              mode: "direct",
              label: "Direct",
              executionCalls: 60,
              share: 1,
            }),
          ]}
        />
      </TooltipProvider>,
    );
    expect(screen.getByText("65%")).toBeTruthy();
    expect(screen.getByText("+4 pts")).toBeTruthy();
    expect(screen.getByText("78 tool calls · 42 discovery")).toBeTruthy();
    expect(screen.getByText("No change")).toBeTruthy();
    expect(
      screen.getByText("1 tool call · 1 discovery · 1 code run"),
    ).toBeTruthy();
    // No calls in the previous period: no change to report.
    expect(screen.getByText("100%")).toBeTruthy();
    expect(screen.getAllByText(/vs previous period/)).toHaveLength(2);
  });
});

describe("ExecutionShareChart", () => {
  const props = {
    lines: [],
    callsWithoutSession: 0,
    loading: false,
    expandedChart: null,
    onExpand: () => {},
  };

  it("explains calls that came without a session instead of an empty chart", () => {
    render(
      <ExecutionShareChart {...props} callsWithoutSession={12} hasCalls />,
    );
    expect(screen.getByText(/sent no session with these calls/)).toBeTruthy();
  });

  it("says the range has no calls when it has none", () => {
    render(<ExecutionShareChart {...props} hasCalls={false} />);
    expect(
      screen.getByText("No discovery or tool calls in the selected range"),
    ).toBeTruthy();
  });
});
