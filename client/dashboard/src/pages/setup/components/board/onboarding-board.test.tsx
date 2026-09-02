import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";

import { TooltipProvider } from "@/components/ui/Tooltip";
import { OnboardingBoard } from "./onboarding-board";

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

  it("puts server-verified tasks in Done and the rest in To Do", () => {
    renderBoard();

    const done = within(screen.getByRole("region", { name: "Done column" }));
    expect(done.getByText("Connect identity provider")).toBeTruthy();
    expect(done.getByText("Verified")).toBeTruthy();

    const todo = within(screen.getByRole("region", { name: "To Do column" }));
    expect(todo.getByText("Directory sync")).toBeTruthy();
    expect(todo.getByText("Set up Platform MCP")).toBeTruthy();
    expect(todo.queryByText("Connect identity provider")).toBeNull();

    expect(screen.getByText("1 of 9 done")).toBeTruthy();
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

    const awaiting = within(
      screen.getByRole("region", { name: "Awaiting Support column" }),
    );
    expect(awaiting.getByText("Confirm traffic")).toBeTruthy();
    expect(awaiting.getByText("security@example.com")).toBeTruthy();
  });
});
