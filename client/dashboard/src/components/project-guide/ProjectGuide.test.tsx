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

vi.mock("./useProjectGuideProgress", () => ({
  useProjectGuideProgress: () => ({
    statusByJourney: statusByJourney.current,
    isPending: progressPending.current,
  }),
}));

import { ProjectGuide } from "./ProjectGuide.tsx";

afterEach(() => {
  cleanup();
  progressPending.current = false;
  statusByJourney.current = {
    "third-party-mcp": "not-started",
    "secret-block": "not-started",
  };
});

describe("ProjectGuide", () => {
  it("offers both journeys, collapsed, with their status", () => {
    render(<ProjectGuide />);
    expect(
      screen.getByRole("button", { name: /Govern third-party MCP usage/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Catch a secret before it leaves/ }),
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
      screen.getByRole("button", { name: /Govern third-party MCP usage/ }),
    );
    expect(screen.getByTestId("journey-body")).toBeTruthy();
    expect(screen.getByText("Pick a server")).toBeTruthy();
    expect(screen.getByText("Pick a server").getAttribute("aria-current")).toBe(
      "step",
    );
  });

  it("links each accordion heading and trigger to its labelled panel", () => {
    render(<ProjectGuide />);
    const trigger = screen.getByRole("button", {
      name: /Govern third-party MCP usage/,
    });

    expect(
      screen.getByRole("heading", { name: /Govern third-party MCP usage/ }),
    ).toBeTruthy();
    const panelId = trigger.getAttribute("aria-controls");
    expect(panelId).toBeTruthy();

    fireEvent.click(trigger);

    const panel = screen.getByRole("region", {
      name: /Govern third-party MCP usage/,
    });
    expect(panel.id).toBe(panelId);
    expect(panel.getAttribute("aria-labelledby")).toBe(trigger.id);
  });

  it("keeps only one card open", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern third-party MCP usage/ }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Catch a secret before it leaves/ }),
    );
    expect(screen.getAllByTestId("journey-body").length).toBe(1);
    expect(screen.getByText("Turn on secret detection")).toBeTruthy();
  });

  it("collapses the open card when clicked again", () => {
    render(<ProjectGuide />);
    const card = screen.getByRole("button", {
      name: /Govern third-party MCP usage/,
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
      screen.getByRole("button", { name: /Govern third-party MCP usage/ }),
    );
    expect(screen.getByText("Deploy it")).toBeTruthy();
  });

  it("shows no current step for a completed journey", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern third-party MCP usage/ }),
    );

    expect(document.querySelectorAll('[aria-current="step"]')).toHaveLength(0);
  });
});
