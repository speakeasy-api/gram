import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";

import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ orgSlug: "acme" }),
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));
vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => ({
    data: { ssoConfigured: true, dsyncConfigured: false },
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: { connected: false }, isLoading: false }),
}));
vi.mock("@gram/client/react-query/listOrganizationUsers.js", () => ({
  useListOrganizationUsers: () => ({ data: { users: [] }, isLoading: false }),
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: () => null,
}));
vi.mock("../onboarding-header", () => ({
  OnboardingHeader: () => null,
}));
vi.mock("../onboarding-footer", () => ({
  OnboardingFooter: () => null,
}));
vi.mock("./task-step", () => ({
  TaskStep: () => null,
}));

afterEach(() => {
  cleanup();
  localStorage.clear();
});

function renderBoard() {
  return render(
    <TooltipProvider>
      <OnboardingBoard />
    </TooltipProvider>,
  );
}

describe("OnboardingBoard", () => {
  it("records that the setup view was opened for the org", () => {
    renderBoard();

    expect(localStorage.getItem("gram-org-welcome-rollout-started:acme")).toBe(
      "true",
    );
  });

  it("groups Quinn's consolidated steps into workstreams", () => {
    renderBoard();

    const expectedTasks = {
      "Connect identity": ["Connect identity provider", "Directory sync"],
      "Observe agents": [
        "Instrument agents",
        "Additional agent configuration",
        "Confirm traffic",
      ],
      "MCP Gateway": [
        "Create plugin marketplace",
        "Distribute MCP servers",
        "Set up Platform MCP",
      ],
      "Secure agent traffic": ["Configure policies"],
    };

    for (const [workstream, tasks] of Object.entries(expectedTasks)) {
      const region = within(screen.getByRole("region", { name: workstream }));
      for (const task of tasks) expect(region.getByText(task)).toBeTruthy();
    }

    const connect = within(
      screen.getByRole("region", { name: "Connect identity" }),
    );
    expect(connect.getByText("Verified")).toBeTruthy();
    expect(screen.getByText("1 of 8 required tasks complete")).toBeTruthy();
  });

  it("restores board state saved for the org", () => {
    localStorage.setItem(
      "gram-onboarding-board:acme",
      JSON.stringify({
        "confirm-traffic": {
          status: "awaiting_support",
          assignee: { kind: "email", email: "security@example.com" },
        },
      }),
    );

    renderBoard();

    const observe = within(
      screen.getByRole("region", { name: "Observe agents" }),
    );
    expect(observe.getByText("Confirm traffic")).toBeTruthy();
    expect(observe.getByText("Awaiting Support")).toBeTruthy();
    expect(observe.getByText("security@example.com")).toBeTruthy();
  });

  it("places every consolidated step in exactly one workstream", () => {
    const configuredIds = ONBOARDING_WORKSTREAMS.flatMap(
      (workstream) => workstream.taskIds,
    );
    expect(new Set(configuredIds).size).toBe(configuredIds.length);
    expect(new Set(configuredIds)).toEqual(
      new Set(ONBOARDING_TASKS.map((task) => task.id)),
    );
  });
});
