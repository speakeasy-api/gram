import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { JourneyId, JourneyStatus } from "./journeys";

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
const journeyBodies = vi.hoisted(() => ({
  thirdParty: vi.fn(),
  secretBlock: vi.fn(),
}));
const bodyError = vi.hoisted(() => ({ current: false }));
const setSearchParams = vi.hoisted(() => vi.fn());
const queryClient = vi.hoisted(() => ({}));
const invalidations = vi.hoisted(() => ({
  activity: vi.fn(),
  results: vi.fn(),
}));

vi.mock("./useProjectGuideProgress", () => ({
  useProjectGuideProgress: () => ({
    statusByJourney: statusByJourney.current,
    isPending: progressPending.current,
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-guide-test",
}));

vi.mock("./ThirdPartyMcpJourney", () => ({
  ThirdPartyMcpJourney: ({
    status,
    onComplete,
    onSwitchJourney,
  }: {
    status: JourneyStatus;
    onComplete: () => void;
    onSwitchJourney: () => void;
  }) => {
    journeyBodies.thirdParty();

    if (bodyError.current) {
      return (
        <div role="alert">
          Third-party journey unavailable <button type="button">Retry</button>
        </div>
      );
    }

    if (status === "done") {
      return (
        <div data-testid="third-party-journey">
          <p>The path is governed.</p>
          <a href="/logs">Open Tool Logs</a>
          <button type="button" onClick={onSwitchJourney}>
            Start the other journey
          </button>
        </div>
      );
    }

    return (
      <div data-testid="third-party-journey">
        <button type="button" onClick={onComplete}>
          Complete third-party journey
        </button>
        <button type="button" onClick={onSwitchJourney}>
          Switch to secret journey
        </button>
      </div>
    );
  },
}));

vi.mock("./SecretBlockJourney", () => ({
  SecretBlockJourney: ({
    onComplete,
    onSwitchJourney,
  }: {
    onComplete: () => void;
    onSwitchJourney: () => void;
  }) => {
    journeyBodies.secretBlock();

    return (
      <div data-testid="secret-block-journey">
        <button type="button" onClick={onComplete}>
          Complete secret-block journey
        </button>
        <button type="button" onClick={onSwitchJourney}>
          Switch to third-party journey
        </button>
      </div>
    );
  },
}));

vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams("showGuide"), setSearchParams],
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => queryClient,
}));

vi.mock("@gram/client/react-query/getMcpServerActivity.js", () => ({
  invalidateGetMcpServerActivity: invalidations.activity,
  invalidateAllGetMcpServerActivity: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  invalidateRiskListResults: invalidations.results,
  invalidateAllRiskListResults: vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    logs: {
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/logs">{children}</a>
      ),
    },
    riskEvents: {
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/risk-events">{children}</a>
      ),
    },
  }),
}));

import { ProjectGuide } from "./ProjectGuide.tsx";

afterEach(() => {
  cleanup();
  progressPending.current = false;
  bodyError.current = false;
  vi.clearAllMocks();
  statusByJourney.current = {
    "third-party-mcp": "not-started",
    "secret-block": "not-started",
  };
});

describe("ProjectGuide", () => {
  it("offers both journeys, collapsed, with their status", () => {
    render(<ProjectGuide />);
    expect(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    ).toBeTruthy();
    expect(screen.getAllByText("Not started").length).toBe(2);
    expect(screen.queryByTestId("journey-body")).toBeNull();
  });

  it("shows derived status per card", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "in-progress",
    };
    render(<ProjectGuide />);
    expect(screen.getByText("Done")).toBeTruthy();
    expect(screen.getByText("In progress")).toBeTruthy();
  });

  it("shows loading status instead of briefly reporting journeys as not started", () => {
    progressPending.current = true;
    render(<ProjectGuide />);

    expect(screen.queryAllByText("Not started")).toHaveLength(0);
    expect(
      screen.getAllByRole("status", { name: /Loading .* journey status/ }),
    ).toHaveLength(2);
  });

  it("expands a card in place", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    expect(screen.getByTestId("journey-body")).toBeTruthy();
    expect(screen.getByTestId("third-party-journey")).toBeTruthy();
  });

  it("links each accordion heading and trigger to its labelled panel", () => {
    render(<ProjectGuide />);
    const trigger = screen.getByRole("button", {
      name: /Govern a third-party MCP/,
    });

    expect(
      screen.getByRole("heading", { name: /Govern a third-party MCP/ }),
    ).toBeTruthy();
    const panelId = trigger.getAttribute("aria-controls");
    expect(panelId).toBeTruthy();

    fireEvent.click(trigger);

    const panel = screen.getByRole("region", {
      name: /Govern a third-party MCP/,
    });
    expect(panel.id).toBe(panelId);
    expect(panel.getAttribute("aria-labelledby")).toBe(trigger.id);
  });

  it("keeps only one card open", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    expect(screen.getAllByTestId("journey-body").length).toBe(1);
    expect(screen.getByTestId("secret-block-journey")).toBeTruthy();
  });

  it("collapses the open card when clicked again", () => {
    render(<ProjectGuide />);
    const card = screen.getByRole("button", {
      name: /Govern a third-party MCP/,
    });
    fireEvent.click(card);
    fireEvent.click(card);
    expect(screen.queryByTestId("journey-body")).toBeNull();
  });

  it("resumes an in-progress journey on its second step", () => {
    statusByJourney.current = {
      "third-party-mcp": "in-progress",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    expect(screen.getByTestId("third-party-journey")).toBeTruthy();
  });

  it("mounts only the expanded journey body", () => {
    render(<ProjectGuide />);

    expect(journeyBodies.thirdParty).not.toHaveBeenCalled();
    expect(journeyBodies.secretBlock).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(journeyBodies.thirdParty).toHaveBeenCalledTimes(1);
    expect(journeyBodies.secretBlock).not.toHaveBeenCalled();
  });

  it("expands one journey column and collapses its sibling to a switchable spine", () => {
    render(<ProjectGuide />);
    const thirdPartyCard = screen.getByTestId(
      "project-guide-third-party-mcp-card",
    );
    const secretBlockCard = screen.getByTestId(
      "project-guide-secret-block-card",
    );

    expect(thirdPartyCard.getAttribute("data-state")).toBe("closed");
    expect(secretBlockCard.getAttribute("data-state")).toBe("closed");

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(thirdPartyCard.getAttribute("data-state")).toBe("open");
    expect(secretBlockCard.getAttribute("data-state")).toBe("spine");
    expect(secretBlockCard.className).toContain("md:w-[54px]");
    expect(screen.getByTestId("third-party-journey")).toBeTruthy();
    expect(screen.queryByTestId("secret-block-journey")).toBeNull();
  });

  it("switches journeys from the body without leaving the guide", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Switch to secret journey" }),
    );

    expect(screen.getByTestId("secret-block-journey")).toBeTruthy();
    expect(screen.queryByTestId("third-party-journey")).toBeNull();
  });

  it("returns to the collapsed journey chooser with the back control", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    fireEvent.click(screen.getByRole("button", { name: "← Back to start" }));

    expect(screen.queryByTestId("journey-body")).toBeNull();
  });

  it("keeps a completed journey open until its derived progress updates", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Complete third-party journey" }),
    );

    expect(screen.getByTestId("third-party-journey")).toBeTruthy();
  });

  it("refreshes the derived progress query after each persisted completion", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Complete third-party journey" }),
    );

    expect(invalidations.activity).toHaveBeenCalledWith(queryClient, [
      { gramProject: "project-guide-test" },
    ]);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Switch to Block a leaked credential mid-prompt journey",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Complete secret-block journey" }),
    );

    expect(invalidations.results).toHaveBeenCalledWith(queryClient, [
      { gramProject: "project-guide-test" },
    ]);
  });

  it("shows the approved one-journey completion treatment with an other-journey action", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(screen.getByText("The path is governed.")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Open Tool Logs" }).getAttribute("href"),
    ).toBe("/logs");
    fireEvent.click(
      screen.getByRole("button", { name: "Start the other journey" }),
    );
    expect(screen.getByTestId("secret-block-journey")).toBeTruthy();
  });

  it("shows a completion state only after both derived journey statuses are done", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "done",
    };
    render(<ProjectGuide />);

    expect(screen.getByTestId("project-guide-complete")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Review what you set up" }),
    ).toBeTruthy();
    expect(
      screen.getByText("This card is replaced on next visit"),
    ).toBeTruthy();
    expect(screen.queryByTestId("journey-body")).toBeNull();
  });

  it("returns to the completed journey records for review", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "done",
    };
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: "Review what you set up" }),
    );

    expect(screen.queryByTestId("project-guide-complete")).toBeNull();
    expect(
      screen.getByTestId("project-guide-third-party-mcp-card"),
    ).toBeTruthy();
  });

  it("returns to normal project home through the existing query gate", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "done",
    };
    render(<ProjectGuide />);

    fireEvent.click(screen.getByRole("button", { name: "Go to project home" }));

    expect(setSearchParams).toHaveBeenCalledWith(new URLSearchParams(), {
      replace: true,
    });
  });

  it("keeps the guide visible when a body exposes a retry path", () => {
    bodyError.current = true;
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(screen.getByRole("alert").textContent).toContain(
      "Third-party journey unavailable",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    ).toBeTruthy();
  });

  it("shows no current step for a completed journey before the completion state", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(document.querySelectorAll('[aria-current="step"]')).toHaveLength(0);
  });
});
