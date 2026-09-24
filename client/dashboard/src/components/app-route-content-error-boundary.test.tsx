import { render, screen } from "@testing-library/react";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Link, MemoryRouter, Route, Routes, useLocation } from "react-router";
import { AppRouteContentErrorBoundary } from "./app-route-content-error-boundary";

function BrokenPage(): JSX.Element {
  throw new Error("Broken sibling route");
}

function SamePathRecoveryPage(): JSX.Element {
  const { search } = useLocation();
  if (!search) throw new Error("Broken navigation entry");
  return <div>Recovered same-path route</div>;
}

function StatefulRouteContent(): JSX.Element {
  const [count, setCount] = useState(0);
  return (
    <>
      <button onClick={() => setCount((current) => current + 1)}>
        Count {count}
      </button>
      <Routes>
        <Route path="/first" element={<div>First route</div>} />
        <Route path="/second" element={<div>Second route</div>} />
      </Routes>
    </>
  );
}

function TestRoutes(): JSX.Element {
  return (
    <>
      <Link to="/working">Open working route</Link>
      <AppRouteContentErrorBoundary>
        <Routes>
          <Route path="/broken" element={<BrokenPage />} />
          <Route path="/working" element={<div>Working route</div>} />
        </Routes>
      </AppRouteContentErrorBoundary>
    </>
  );
}

describe("AppRouteContentErrorBoundary", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not remount healthy ancestor content on navigation", async () => {
    render(
      <MemoryRouter initialEntries={["/first"]}>
        <Link to="/second">Open second route</Link>
        <AppRouteContentErrorBoundary>
          <StatefulRouteContent />
        </AppRouteContentErrorBoundary>
      </MemoryRouter>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Count 0" }));
    await userEvent.click(
      screen.getByRole("link", { name: "Open second route" }),
    );

    expect(screen.getByRole("button", { name: "Count 1" })).toBeTruthy();
    expect(screen.getByText("Second route")).toBeTruthy();
  });

  it("recovers on a new same-path query navigation entry", async () => {
    render(
      <MemoryRouter initialEntries={["/same"]}>
        <Link to="/same?retry=1">Retry same route</Link>
        <AppRouteContentErrorBoundary>
          <Routes>
            <Route path="/same" element={<SamePathRecoveryPage />} />
          </Routes>
        </AppRouteContentErrorBoundary>
      </MemoryRouter>,
    );

    expect(await screen.findByText("Error loading Page")).toBeTruthy();
    await userEvent.click(
      screen.getByRole("link", { name: "Retry same route" }),
    );

    expect(await screen.findByText("Recovered same-path route")).toBeTruthy();
    expect(screen.queryByText("Error loading Page")).toBeNull();
  });

  it("recovers from a sibling route error after navigation", async () => {
    render(
      <MemoryRouter initialEntries={["/broken"]}>
        <TestRoutes />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Error loading Page")).toBeTruthy();

    await userEvent.click(
      screen.getByRole("link", { name: "Open working route" }),
    );

    expect(await screen.findByText("Working route")).toBeTruthy();
    expect(screen.queryByText("Error loading Page")).toBeNull();
  });
});
