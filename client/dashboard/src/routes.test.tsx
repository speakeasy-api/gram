import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { LegacyDataRedirect } from "./pages/data-exports/LegacyDataRedirect";
import { orgRoutePaths, useRoutes } from "./routes";

function GuideHref(): JSX.Element {
  const routes = useRoutes();
  return <output>{routes.guide.href()}</output>;
}

function LocationPath(): JSX.Element {
  const location = useLocation();
  return <output>{location.pathname + location.search + location.hash}</output>;
}

describe("project routes", () => {
  it("exposes the standalone guide route", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <GuideHref />
      </MemoryRouter>,
    );

    expect(screen.getByText("/org/projects/project/guide")).toBeTruthy();
  });
});

describe("organization routes", () => {
  it("keeps the legacy Data URL and redirects it to Event Feed", async () => {
    expect(orgRoutePaths).toContain("data");
    render(
      <MemoryRouter initialEntries={["/org/data?filter=errors#latest"]}>
        <Routes>
          <Route path="/org/data" element={<LegacyDataRedirect />} />
          <Route path="*" element={<LocationPath />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(
      await screen.findByText("/org/data/event-feed?filter=errors#latest"),
    ).toBeTruthy();
  });
});
