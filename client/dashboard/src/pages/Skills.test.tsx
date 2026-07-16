import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Skills from "./Skills";

const testState = vi.hoisted(() => ({
  productFeatureOptions: undefined as
    | { staleTime?: number; throwOnError?: boolean }
    | undefined,
  projectId: "project_a",
  skillsEnabled: undefined as boolean | undefined,
  isLoading: false,
  error: null as Error | null,
  refetch: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: testState.projectId }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: (
    _request: unknown,
    _security: unknown,
    options: { staleTime?: number; throwOnError?: boolean } | undefined,
  ) => {
    testState.productFeatureOptions = options;
    return {
      data:
        testState.skillsEnabled === undefined
          ? undefined
          : { skillsEnabled: testState.skillsEnabled },
      error: testState.error,
      isLoading: testState.isLoading,
      refetch: testState.refetch,
    };
  },
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    resourceId,
    scope,
  }: {
    children: ReactNode;
    resourceId?: string;
    scope: string;
  }) => (
    <div
      data-testid="scope-gate"
      data-resource-id={resourceId}
      data-scope={scope}
    >
      {children}
    </div>
  ),
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }
  function Header({ children }: { children?: ReactNode }) {
    return <header>{children}</header>;
  }
  function Section({ children }: { children: ReactNode }) {
    return <section>{children}</section>;
  }
  return {
    Page: Object.assign(Page, {
      Header: Object.assign(Header, {
        Breadcrumbs: () => <nav>Breadcrumbs</nav>,
      }),
      Body: ({ children }: { children: ReactNode }) => <main>{children}</main>,
      Section: Object.assign(Section, {
        Title: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
        Description: ({ children }: { children: ReactNode }) => (
          <p>{children}</p>
        ),
        Body: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      }),
    }),
  };
});

vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  Icon: () => <span data-testid="skills-icon" />,
}));

function renderPage(): void {
  render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route path="/" element={<Skills />}>
          <Route index element={<div>Skills index route</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  testState.skillsEnabled = undefined;
  testState.isLoading = false;
  testState.error = null;
  testState.refetch.mockReset();
});

afterEach(cleanup);

describe("Skills", () => {
  it("preserves the Coming Soon page and project gate when Skills is confirmed disabled", () => {
    testState.skillsEnabled = false;
    renderPage();

    expect(screen.getByTestId("scope-gate").getAttribute("data-scope")).toBe(
      "project:read",
    );
    expect(
      screen.getByTestId("scope-gate").getAttribute("data-resource-id"),
    ).toBeNull();
    expect(screen.getByText("Coming Soon")).toBeTruthy();
    expect(screen.getByText("No skills yet")).toBeTruthy();
  });

  it("renders a distinct loading state without choosing a scope gate", () => {
    testState.isLoading = true;
    renderPage();

    expect(screen.getByLabelText("Loading Skills")).toBeTruthy();
    expect(screen.queryByTestId("scope-gate")).toBeNull();
    expect(screen.queryByText("Coming Soon")).toBeNull();
  });

  it("renders a distinct feature-query error and retries", () => {
    testState.error = new Error("network");
    renderPage();

    expect(screen.getByText("Unable to load Skills availability")).toBeTruthy();
    expect(screen.queryByText("Coming Soon")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });

  it("renders the enabled outlet behind the project-scoped Skills read gate", () => {
    testState.skillsEnabled = true;
    renderPage();

    expect(screen.getByTestId("scope-gate").getAttribute("data-scope")).toBe(
      "skill:read",
    );
    expect(
      screen.getByTestId("scope-gate").getAttribute("data-resource-id"),
    ).toBe("project_a");
    expect(screen.getByText("Skills index route")).toBeTruthy();
    expect(testState.productFeatureOptions?.staleTime).toBe(30_000);
    expect(testState.productFeatureOptions?.throwOnError).toBe(false);
  });
});
