import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";

import { GuideEntryRedirect } from "./GuideEntryRedirect";

const projects = vi.hoisted(() => ({
  current: [{ id: "p-default", name: "Default", slug: "default" }],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: "acme", projects: projects.current }),
}));

function LocationDisplay(): JSX.Element {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}</output>;
}

afterEach(() => {
  cleanup();
  localStorage.clear();
  projects.current = [{ id: "p-default", name: "Default", slug: "default" }];
});

describe("GuideEntryRedirect", () => {
  it("redirects /guide to the preferred project's guide", () => {
    projects.current = [
      { id: "p-alpha", name: "Alpha", slug: "alpha" },
      { id: "p-default", name: "Default", slug: "default" },
    ];
    localStorage.setItem("preferredProject", "alpha");

    render(
      <MemoryRouter initialEntries={["/guide"]}>
        <Routes>
          <Route path="/guide" element={<GuideEntryRedirect />} />
          <Route path="*" element={<LocationDisplay />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/projects/alpha/guide",
    );
  });

  it("falls back to org home when the org has no projects", () => {
    projects.current = [];

    render(
      <MemoryRouter initialEntries={["/guide"]}>
        <Routes>
          <Route path="/guide" element={<GuideEntryRedirect />} />
          <Route path="*" element={<LocationDisplay />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("location").textContent).toBe("/acme");
  });
});
