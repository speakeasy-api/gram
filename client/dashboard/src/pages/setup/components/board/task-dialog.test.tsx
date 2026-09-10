import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOrgRoutes } from "@/routes";
import { Suspense } from "react";
import { TaskDialog } from "./task-dialog";
import { ONBOARDING_TASKS, type OnboardingTaskId } from "./tasks";

vi.mock("../../SetupTaskPage", () => ({
  default: () => <p>Standalone guided page</p>,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example-org", projectSlug: "default" }),
}));
vi.mock("./assignee-picker", () => ({ AssigneePicker: () => null }));
vi.mock("./remind-button", () => ({ RemindButton: () => null }));
vi.mock("./task-step", () => ({ TaskStep: () => <p>Inline task content</p> }));

afterEach(cleanup);

function RouteState() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname + location.search}
    </output>
  );
}

function renderTask(id: OnboardingTaskId, projectSlug?: string) {
  const definition = ONBOARDING_TASKS.find((task) => task.id === id)!;
  return render(
    <MemoryRouter initialEntries={["/example-org/setup"]}>
      <RouteState />
      <TaskDialog
        task={{ ...definition, status: "todo", verified: false, hidden: false }}
        projectSlug={projectSlug}
        isReminding={false}
        onClose={vi.fn<() => void>()}
        onOpenTask={vi.fn<() => void>()}
        onSetStatus={vi.fn<() => void>()}
        onAssign={vi.fn<() => void>()}
        onRemind={vi.fn<() => void>()}
      />
    </MemoryRouter>,
  );
}

function GuidedRoute() {
  const routes = useOrgRoutes();
  const Page = routes.setupTask.component!;
  return (
    <Suspense fallback={null}>
      <Page />
    </Suspense>
  );
}

describe("guided setup from the board dialog", () => {
  it("retains the standalone guided page in the actual route definition", async () => {
    render(
      <MemoryRouter>
        <GuidedRoute />
      </MemoryRouter>,
    );
    expect(await screen.findByText("Standalone guided page")).toBeTruthy();
  });

  it.each([
    ["identity-provider", "idp"],
    ["anthropic-observability", "anthropic-observability"],
    ["anthropic-admin-controls", "anthropic-admin-controls"],
    ["instrument-agents", "other-platforms"],
    ["additional-agent-config", "integrations"],
    ["distribute-servers", "distribute-servers"],
    ["configure-policies", "policies"],
    ["platform-mcp", "platform-mcp"],
  ] as const)("links %s through the actual organization route", (id, slug) => {
    renderTask(id, "selected project");
    const link = screen.getByRole("link", { name: "Open guided setup" });
    const destination = `/example-org/setup/${slug}?projectSlug=selected+project`;
    expect(link.getAttribute("href")).toBe(destination);
    expect(screen.getByText("Inline task content")).toBeTruthy();
    fireEvent.click(link);
    expect(screen.getByTestId("location").textContent).toBe(destination);
  });

  it("omits the query when no project was selected", () => {
    renderTask("platform-mcp");
    expect(
      screen
        .getByRole("link", { name: "Open guided setup" })
        .getAttribute("href"),
    ).toBe("/example-org/setup/platform-mcp");
  });

  it.each([
    "connect-idp",
    "directory-sync",
    "create-marketplace",
    "confirm-traffic",
  ] as const)(
    "keeps unsupported %s inline without a dead guided link",
    (id) => {
      renderTask(id);
      expect(
        screen.queryByRole("link", { name: "Open guided setup" }),
      ).toBeNull();
      expect(screen.getByText("Inline task content")).toBeTruthy();
    },
  );
});
