import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";

import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";
import { countTasksBelow } from "./count-tasks-below";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ orgSlug: "acme" }),
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setupTask: { Link: () => null },
  }),
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
  useSession: () => ({
    user: { id: "user-me", email: "dev@example.com" },
  }),
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

describe("countTasksBelow", () => {
  it("counts only cards extending below the visible task list", () => {
    const taskList = document.createElement("div");
    Object.defineProperties(taskList, {
      clientHeight: { value: 200 },
      scrollHeight: { value: 400 },
    });
    vi.spyOn(taskList, "getBoundingClientRect").mockReturnValue({
      bottom: 200,
    } as DOMRect);

    for (const bottom of [150, 240, 380]) {
      const card = document.createElement("article");
      vi.spyOn(card, "getBoundingClientRect").mockReturnValue({
        bottom,
      } as DOMRect);
      taskList.append(card);
    }

    expect(countTasksBelow(taskList)).toBe(2);
  });
});

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
      "Connect identity": [
        "Set up identity provider",
        "Connect identity provider",
        "Directory sync",
      ],
      "Observe agents": [
        "Set up Anthropic observability",
        "Set up observability in other platforms",

        "Additional agent configuration",
        "Confirm traffic",
      ],
      "MCP Gateway": [
        "Create plugin marketplace",
        "Distribute MCP servers",
        "Set up Platform MCP",
      ],
      "Secure agent traffic": [
        "Set up Anthropic admin controls",
        "Configure policies",
      ],
    };

    for (const [workstream, tasks] of Object.entries(expectedTasks)) {
      const region = within(screen.getByRole("region", { name: workstream }));
      for (const task of tasks) expect(region.getByText(task)).toBeTruthy();
    }

    const connect = within(
      screen.getByRole("region", { name: "Connect identity" }),
    );
    expect(connect.getByText("Verified")).toBeTruthy();
    expect(screen.getByText("1 of 11 required tasks complete")).toBeTruthy();
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

  it("filters the consolidated board to tasks assigned to the viewer", () => {
    localStorage.setItem(
      "gram-onboarding-board:acme",
      JSON.stringify({
        "connect-idp": {
          assignee: {
            kind: "user",
            userId: "user-me",
            name: "Dev User",
            email: "dev@example.com",
          },
        },
        "directory-sync": {
          assignee: {
            kind: "email",
            email: "someone-else@example.com",
          },
        },
      }),
    );

    renderBoard();
    fireEvent.click(screen.getByRole("switch", { name: "My tasks" }));

    expect(screen.getByText("Connect identity provider")).toBeTruthy();
    expect(screen.queryByText("Directory sync")).toBeNull();
    expect(
      screen.queryByText("Set up observability in other platforms"),
    ).toBeNull();
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
