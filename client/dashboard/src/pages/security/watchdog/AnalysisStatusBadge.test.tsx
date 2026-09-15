import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RiskAnalysisStatusResult } from "@gram/client/models/components/riskanalysisstatusresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnalysisStatusBadge } from "./AnalysisStatusBadge";

const useRiskAnalysisStatus = vi.hoisted(() => vi.fn());

vi.mock("@gram/client/react-query/riskAnalysisStatus.js", () => ({
  useRiskAnalysisStatus,
}));

const NOW = new Date("2026-09-15T12:00:00Z");

function loaded(data: RiskAnalysisStatusResult) {
  return { data, isPending: false, isSuccess: true, isError: false };
}

function renderBadge() {
  return render(
    <TooltipProvider>
      <AnalysisStatusBadge />
    </TooltipProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("AnalysisStatusBadge", () => {
  it("shows a live indicator while a run is in flight", () => {
    useRiskAnalysisStatus.mockReturnValue(
      loaded({
        state: "running",
        runningSince: new Date("2026-09-15T11:59:30Z"),
      }),
    );
    renderBadge();
    expect(screen.getByText("Analyzing now")).toBeTruthy();
  });

  it("shows the relative time of the last completed run", () => {
    vi.spyOn(Date, "now").mockReturnValue(NOW.getTime());
    useRiskAnalysisStatus.mockReturnValue(
      loaded({
        state: "idle",
        lastRunStartedAt: new Date("2026-09-15T11:55:00Z"),
        lastRunAt: new Date("2026-09-15T11:56:00Z"),
        lastRunOutcome: "completed",
      }),
    );
    renderBadge();
    expect(screen.getByText("Last analyzed 4m ago")).toBeTruthy();
  });

  it("treats a continued-as-new run as a normal completion", () => {
    vi.spyOn(Date, "now").mockReturnValue(NOW.getTime());
    useRiskAnalysisStatus.mockReturnValue(
      loaded({
        state: "idle",
        lastRunAt: new Date("2026-09-15T10:00:00Z"),
        lastRunOutcome: "continued_as_new",
      }),
    );
    renderBadge();
    expect(screen.getByText("Last analyzed 2h ago")).toBeTruthy();
  });

  it("appends an abnormal outcome to the last-run label", () => {
    vi.spyOn(Date, "now").mockReturnValue(NOW.getTime());
    useRiskAnalysisStatus.mockReturnValue(
      loaded({
        state: "idle",
        lastRunAt: new Date("2026-09-15T11:30:00Z"),
        lastRunOutcome: "timed_out",
      }),
    );
    renderBadge();
    expect(screen.getByText("Last analyzed 30m ago · timed out")).toBeTruthy();
  });

  it("says so when nothing has been analyzed yet", () => {
    useRiskAnalysisStatus.mockReturnValue(loaded({ state: "never" }));
    renderBadge();
    expect(screen.getByText("No analysis yet")).toBeTruthy();
  });

  it("renders a placeholder while loading", () => {
    useRiskAnalysisStatus.mockReturnValue({
      data: undefined,
      isPending: true,
      isSuccess: false,
      isError: false,
    });
    const { container } = renderBadge();
    expect(container.firstChild).not.toBeNull();
    expect(screen.queryByText(/analy/i)).toBeNull();
  });

  it("renders nothing when the status query fails", () => {
    useRiskAnalysisStatus.mockReturnValue({
      data: undefined,
      isPending: false,
      isSuccess: false,
      isError: true,
      error: new Error("status is down"),
    });
    const { container } = renderBadge();
    expect(container.firstChild).toBeNull();
  });
});
