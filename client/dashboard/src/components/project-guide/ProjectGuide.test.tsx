import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProjectGuideRun } from "./ProjectGuideRun";
import {
  PROJECT_GUIDE_JOURNEYS,
  type JourneyId,
  type JourneyStatus,
} from "./journeys";
import type {
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "./projectGuideMachine";

const statusByJourney = vi.hoisted(
  () =>
    ({
      current: {
        "third-party-mcp": "not-started",
        "secret-block": "not-started",
      },
    }) as { current: Record<JourneyId, JourneyStatus> },
);
const progressPending = vi.hoisted(() => ({ current: false }));
const setSearchParams = vi.hoisted(() => vi.fn());

vi.mock("./useProjectGuideProgress", () => ({
  useProjectGuideProgress: () => ({
    statusByJourney: statusByJourney.current,
    isPending: progressPending.current,
  }),
}));

vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams("showGuide"), setSearchParams],
}));

import { ProjectGuide } from "./ProjectGuide";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  progressPending.current = false;
  statusByJourney.current = {
    "third-party-mcp": "not-started",
    "secret-block": "not-started",
  };
});

describe("ProjectGuide", () => {
  it("renders the approved opening with both selectable journeys", () => {
    render(<ProjectGuide />);

    expect(
      screen.getByRole("heading", {
        name: "Put your agent traffic under control",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    ).toBeTruthy();
    expect(
      screen.getByTestId("project-guide-secret-block-card").textContent,
    ).toContain("Not started");
    expect(
      screen.getByTestId("project-guide-third-party-mcp-card").textContent,
    ).toContain("Not started");
  });

  it("keeps the other journey switchable when a selected path opens", () => {
    render(<ProjectGuide />);

    const openingControl = screen.getByRole("button", {
      name: /Govern a third-party MCP/,
    });
    const controlledRegionId = openingControl.getAttribute("aria-controls");
    expect(openingControl.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(openingControl);

    const activeRegion = screen.getByRole("region", {
      name: "Govern a third-party MCP",
    });
    expect(activeRegion.id).toBe(controlledRegionId);
    expect(
      screen
        .getByRole("button", { name: "← Back to start" })
        .getAttribute("aria-expanded"),
    ).toBe("true");
    expect(
      screen
        .getByRole("button", { name: "← Back to start" })
        .getAttribute("aria-controls"),
    ).toBe(controlledRegionId);

    const switchControl = screen.getByRole("button", {
      name: "Switch to Block a leaked credential mid-prompt",
    });
    expect(switchControl.getAttribute("aria-expanded")).toBe("false");
    expect(switchControl.getAttribute("aria-controls")).toBe(
      "project-guide-secret-block-content",
    );

    expect(
      screen.getByRole("heading", { name: "Govern a third-party MCP" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Switch to Block a leaked credential mid-prompt",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "The catalog lists servers from the official MCP Registry. Installing one creates a governed endpoint in front of the vendor's server — the vendor's URL is already known, and nothing upstream changes.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Ready · Pick a server from the catalog");

    fireEvent.click(switchControl);

    expect(
      screen.getByRole("heading", {
        name: "Block a leaked credential mid-prompt",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", {
        name: "Block a leaked credential mid-prompt",
      }).id,
    ).toBe("project-guide-secret-block-content");
  });

  it("dispatches START and the matching operation signal from the primary button", () => {
    const signals: ProjectGuideOperationSignal[] = [];
    render(
      <ProjectGuide
        onOperationSignal={(signal) => {
          signals.push(signal);
        }}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(signals).toEqual([
      {
        type: "start",
        scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
      },
    ]);
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Started · Pick a server from the catalog");
  });

  it("renders checkpoint, waiting, and observed-event completion from adapter reports", () => {
    let report: (report: ProjectGuideOperationReport) => void = () => undefined;
    let activeScope: ProjectGuideOperationScope | null = null;
    render(
      <ProjectGuide
        onOperationSignal={(signal, sendReport) => {
          report = sendReport;
          activeScope = signal.scope;
          if (signal.type !== "start") return;
          if (signal.scope.step === 0) {
            sendReport({
              type: "success",
              scope: signal.scope,
              result: "Server installed",
            });
          }
          if (signal.scope.step === 1) {
            sendReport({
              type: "success",
              scope: signal.scope,
              result: "Endpoint verified",
            });
          }
        }}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "checkpoint",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "I've completed this step" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "I've completed this step" }),
    );

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "waiting",
    );
    expect(screen.getByRole("status").textContent).toContain("Listening");
    if (!activeScope) throw new Error("expected waiting operation scope");
    const waitingScope = activeScope;

    act(() => {
      report({
        type: "event",
        scope: waitingScope,
        event: {
          kind: "Governed call",
          tone: "allow",
          title: "linear.tools/list",
          rows: [{ key: "access", value: "allowed" }],
          note: "Recorded before forwarding.",
        },
      });
    });

    expect(
      screen.getByRole("heading", { name: "The path is governed." }),
    ).toBeTruthy();
    expect(screen.getByText("linear.tools/list")).toBeTruthy();
  });

  it("renders an adapter error and retries through the same operation port", () => {
    const signals: ProjectGuideOperationSignal[] = [];
    let failStart = true;
    render(
      <ProjectGuide
        onOperationSignal={(signal, report) => {
          signals.push(signal);
          if (signal.type === "start" && failStart) {
            failStart = false;
            report({
              type: "error",
              scope: signal.scope,
              message: "Catalog unavailable",
            });
          }
        }}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));

    expect(screen.getByRole("alert").textContent).toContain(
      "Catalog unavailable",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(signals.at(-1)).toMatchObject({
      type: "retry",
      scope: { step: 0, attempt: 1 },
    });
  });

  it("renders supplied current content, output, event, and action callbacks", () => {
    const onPrimaryAction = vi.fn();
    const onSecondaryAction = vi.fn();

    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        status="not-started"
        regionId="fixture-run"
        currentStep={2}
        currentContent={<p>Coordinator-provided checkpoint</p>}
        output={<span>Coordinator output line</span>}
        eventCard={<section>Coordinator event card</section>}
        primaryAction={{
          label: "Continue run",
          onClick: () => {
            onPrimaryAction();
          },
        }}
        secondaryAction={{
          label: "Cancel run",
          onClick: () => {
            onSecondaryAction();
          },
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(screen.getByText("Coordinator-provided checkpoint")).toBeTruthy();
    expect(screen.getByText("Coordinator output line")).toBeTruthy();
    expect(screen.getByText("Coordinator event card")).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "Govern a third-party MCP" }).id,
    ).toBe("fixture-run");

    fireEvent.click(screen.getByRole("button", { name: "Continue run" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel run" }));

    expect(onPrimaryAction).toHaveBeenCalledOnce();
    expect(onSecondaryAction).toHaveBeenCalledOnce();
  });

  it("keeps activity history bounded and scrolls to the latest output", () => {
    const journey = PROJECT_GUIDE_JOURNEYS[1]!;
    const { rerender } = render(
      <ProjectGuideRun
        journey={journey}
        status="in-progress"
        regionId="scroll-run"
        displayState="running"
        completedSteps={[]}
        currentStep={0}
        output={<span>First output</span>}
        primaryAction={{ label: "Pause" }}
        onSwitchJourney={() => undefined}
      />,
    );
    const activity = screen.getByRole("log", { name: "Journey A activity" });
    Object.defineProperty(activity, "scrollHeight", {
      configurable: true,
      value: 400,
    });
    activity.scrollTop = 0;

    rerender(
      <ProjectGuideRun
        journey={journey}
        status="in-progress"
        regionId="scroll-run"
        displayState="running"
        completedSteps={[]}
        currentStep={0}
        output={<span>Latest output</span>}
        primaryAction={{ label: "Pause" }}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(activity.className).toContain("max-h-");
    expect(activity.scrollTop).toBe(400);
  });

  it("keeps elapsed listening ticks out of the polite live status", () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        status="in-progress"
        regionId="waiting-run"
        displayState="waiting"
        completedSteps={[0, 1, 2, 3]}
        currentStep={4}
        output={<span>Listening started</span>}
        primaryAction={{ label: "Pause listening" }}
        listeningElapsedSeconds={12}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(screen.getByRole("status").textContent).toBe(
      "Listening for an event",
    );
    expect(screen.getByText("12s elapsed").getAttribute("aria-hidden")).toBe(
      "true",
    );
  });

  it("resumes artifact progress without inventing output or an observed event", () => {
    statusByJourney.current = {
      "third-party-mcp": "in-progress",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(
      screen.getByRole("log", { name: "Journey A activity" }),
    ).toBeTruthy();
    expect(
      screen.getByText("Ready · Confirm the governed endpoint"),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        "Endpoint verified, client connected, no calls recorded.",
      ),
    ).toBeNull();
    expect(screen.queryByText("The call you watched")).toBeNull();
  });

  it("keeps unavailable progress out of an empty or completed display", () => {
    statusByJourney.current = {
      "third-party-mcp": "unreadable",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);

    expect(screen.getAllByText("Progress unavailable")).toHaveLength(2);
    expect(
      screen.getByTestId("project-guide-third-party-mcp-card").textContent,
    ).not.toContain("Not started");
  });

  it("shows completion actions after both derived journey artifacts are done", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "done",
    };
    render(<ProjectGuide />);

    expect(
      screen.getByRole("heading", {
        name: "Both journeys are on the record.",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Review what you set up" }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Go to project home" }));

    expect(setSearchParams).toHaveBeenCalledWith(new URLSearchParams(), {
      replace: true,
    });
  });

  it("shows the approved completed-journey summary and keeps the other path available", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(
      screen.getByText(
        "Your client now reaches linear through an endpoint you own. Tool lists are filtered to what each caller may use, every call lands in tool logs, and the vendor's server never changed. Remove the server and the path closes.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open tool logs" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Start the other journey" }),
    ).toBeTruthy();
  });
});
