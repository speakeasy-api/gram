import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  assistantsAvailable: true,
  terminalHome: "disabled" as "enabled" | "disabled" | "loading",
  canReadAssistants: true,
  connectionAccess: "off" as "pending" | "on" | "off",
  widgetClassNames: [] as string[],
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.terminalHome }),
}));
vi.mock("@/components/home/HomeTerminal", () => ({
  HomeTerminal: ({ className }: { className: string }) => (
    <div data-testid="home-terminal" className={className} />
  ),
}));
vi.mock("@/components/home/HomeWidgets", () => ({
  HomeWidgets: ({ classNames }: { classNames: string[] }) => {
    state.widgetClassNames = classNames;
    return <div data-testid="home-widgets" />;
  },
}));
vi.mock("@/components/home/useConnectionProblems", () => ({
  useConnectionProblemsAccess: () => state.connectionAccess,
}));
vi.mock("@/hooks/useAssistantsAvailability", () => ({
  useAssistantsAvailable: () => state.assistantsAvailable,
}));
vi.mock("@/components/project/ProjectDashboard", () => ({
  ProjectDashboard: () => <div data-testid="project-dashboard" />,
}));
vi.mock("@/pages/chat/Chat", () => ({
  ChatLanding: () => <div data-testid="chat-landing" />,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("@/components/insights-context", () => ({
  useHideInsightsDock: () => undefined,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children, scope }: { children: ReactNode; scope: string }) =>
    scope === "assistant:read" && !state.canReadAssistants ? null : (
      <>{children}</>
    ),
}));
vi.mock("@/components/page-layout", () => {
  const Page = Object.assign(
    ({ children }: { children: ReactNode }) => <>{children}</>,
    {
      Header: Object.assign(
        ({ children }: { children?: ReactNode }) => <>{children}</>,
        { Breadcrumbs: () => null },
      ),
      Body: ({ children }: { children: ReactNode }) => <>{children}</>,
    },
  );
  return { Page };
});
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: string[]) =>
      !scopes.includes("assistant:read") || state.canReadAssistants,
    hasAnyScopeInProject: () => true,
    isLoading: false,
  }),
}));
import Home from "./Home.tsx";

afterEach(() => {
  cleanup();
  state.assistantsAvailable = true;
  state.terminalHome = "disabled";
  state.canReadAssistants = true;
  state.connectionAccess = "off";
  state.widgetClassNames = [];
});

const classes = (el: Element): string[] =>
  (el.getAttribute("class") ?? "").split(/\s+/);

function renderHome(): void {
  render(
    <MemoryRouter>
      <Home />
    </MemoryRouter>,
  );
}

describe("Home", () => {
  it("always renders the normal project overview", () => {
    render(
      <MemoryRouter>
        <Home />
      </MemoryRouter>,
    );
    expect(screen.getByTestId("chat-landing")).toBeTruthy();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
  });

  it("leaves out Ask anything on a deployment without assistants", () => {
    state.assistantsAvailable = false;
    render(
      <MemoryRouter>
        <Home />
      </MemoryRouter>,
    );
    expect(screen.queryByTestId("chat-landing")).toBeNull();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
  });

  it("renders the terminal homepage when terminal-home is on", () => {
    state.terminalHome = "enabled";
    renderHome();
    expect(screen.getByTestId("home-terminal")).toBeTruthy();
    expect(screen.getByTestId("home-widgets")).toBeTruthy();
    expect(screen.queryByTestId("project-dashboard")).toBeNull();
  });

  it("keeps the normal overview when terminal-home is on but assistants are unavailable", () => {
    state.terminalHome = "enabled";
    state.assistantsAvailable = false;
    renderHome();
    expect(screen.queryByTestId("home-terminal")).toBeNull();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
  });

  it("keeps the normal overview when terminal-home is on but the viewer lacks assistant:read", () => {
    state.terminalHome = "enabled";
    state.canReadAssistants = false;
    renderHome();
    expect(screen.queryByTestId("home-terminal")).toBeNull();
    expect(screen.queryByTestId("chat-landing")).toBeNull();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
  });

  it("shows a loading state, not either layout, while the flag is loading", () => {
    state.terminalHome = "loading";
    renderHome();
    expect(screen.queryByTestId("home-terminal")).toBeNull();
    expect(screen.queryByTestId("project-dashboard")).toBeNull();
    expect(
      screen.getByRole("status", { name: "Loading the project home" }),
    ).toBeTruthy();
  });

  describe("grid", () => {
    const grid = () => screen.getByTestId("home-terminal").parentElement!;

    it.each(["off", "pending"] as const)(
      "keeps three columns when connection rows are %s",
      (access) => {
        state.terminalHome = "enabled";
        state.connectionAccess = access;
        renderHome();
        expect(classes(grid())).toContain("lg:grid-cols-3");
        expect(classes(grid())).not.toContain("lg:grid-cols-12");
        expect(classes(screen.getByTestId("home-terminal"))).toEqual(
          expect.arrayContaining(["lg:col-span-2", "lg:col-start-2"]),
        );
        expect(state.widgetClassNames).toEqual([
          "lg:col-start-1 lg:row-start-1",
          "lg:col-start-1 lg:row-start-2",
          "",
          "",
          "",
        ]);
      },
    );

    it("gives Needs attention two rows and the other four the bottom row for viewers with connection rows", () => {
      state.terminalHome = "enabled";
      state.connectionAccess = "on";
      renderHome();
      expect(classes(grid())).toEqual(
        expect.arrayContaining(["lg:grid-cols-12", "lg:grid-rows-3"]),
      );
      expect(classes(screen.getByTestId("home-terminal"))).toEqual(
        expect.arrayContaining([
          "lg:col-span-8",
          "lg:col-start-5",
          "lg:row-span-2",
          "lg:row-start-1",
        ]),
      );
      const [attention, ...bottom] = state.widgetClassNames;
      expect(attention!.split(" ")).toEqual(
        expect.arrayContaining([
          "lg:col-span-4",
          "lg:row-span-2",
          "lg:row-start-1",
        ]),
      );
      expect(bottom).toHaveLength(4);
      for (const c of bottom) {
        expect(c.split(" ")).toEqual(
          expect.arrayContaining(["lg:col-span-3", "lg:row-start-3"]),
        );
      }
    });
  });
});
