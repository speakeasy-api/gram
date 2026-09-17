import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { LegacyDataRedirect } from "./pages/data-exports/LegacyDataRedirect";
import {
  ShadowMCPLegacyRedirect,
  ShadowMCPServerLegacyRedirect,
} from "./pages/shadow-ai/ShadowAI";
import { orgRoutePaths, useRoutes } from "./routes";

function GuideHref(): JSX.Element {
  const routes = useRoutes();
  return <output>{routes.guide.href()}</output>;
}

function ProjectRouteHrefs(): JSX.Element {
  const routes = useRoutes();
  return (
    <output data-testid="project-route-hrefs">
      {Object.values(routes)
        .map((route) => route.href())
        .join("\n")}
    </output>
  );
}

function ShadowAIHrefs(): JSX.Element {
  const routes = useRoutes();
  return (
    <output data-testid="shadow-ai-hrefs">
      {[
        routes.shadowAI.harnesses.href(),
        routes.shadowAI.mcps.href(),
        routes.shadowAI.mcps.detail.href("server-slug"),
      ].join("\n")}
    </output>
  );
}

function LocationPath(): JSX.Element {
  const location = useLocation();
  return <output>{location.pathname + location.search + location.hash}</output>;
}

function GoToExploreDemo(): JSX.Element {
  const routes = useRoutes();
  return (
    <>
      <button
        type="button"
        onClick={() => {
          routes.exploreDemo.goTo();
        }}
      >
        go
      </button>
      <LocationPath />
    </>
  );
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

  it("exposes the Shadow AI section with Shadow MCP as one of its tabs", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <ProjectRouteHrefs />
      </MemoryRouter>,
    );

    const hrefs = screen.getByTestId("project-route-hrefs").textContent ?? "";
    expect(hrefs).toContain("/org/projects/project/shadow-ai");
    // The Shadow MCP paths predate the section and are in bookmarks and block
    // messages, so they stay routable and redirect.
    expect(hrefs).toContain("/org/projects/project/shadow-mcp");
  });

  it("nests the Shadow MCP tab and server detail under the section", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <ShadowAIHrefs />
      </MemoryRouter>,
    );

    expect(
      (screen.getByTestId("shadow-ai-hrefs").textContent ?? "").split("\n"),
    ).toEqual([
      "/org/projects/project/shadow-ai/harnesses",
      "/org/projects/project/shadow-ai/mcps",
      "/org/projects/project/shadow-ai/mcps/server-slug",
    ]);
  });

  // Block messages and bookmarks carry the old paths with a query and a
  // fragment; both must survive the redirect or the deep link lands at the top.
  it("redirects the legacy Shadow MCP paths into the section keeping search and hash", async () => {
    render(
      <MemoryRouter
        initialEntries={[
          "/org/projects/project/shadow-mcp?status=blocked#tools",
        ]}
      >
        <Routes>
          <Route
            path="/org/projects/project/shadow-mcp"
            element={<ShadowMCPLegacyRedirect />}
          />
          <Route path="*" element={<LocationPath />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(
      await screen.findByText(
        "/org/projects/project/shadow-ai/mcps?status=blocked#tools",
      ),
    ).toBeTruthy();
  });

  it("redirects a legacy Shadow MCP server path to the nested detail", async () => {
    render(
      <MemoryRouter
        initialEntries={[
          "/org/projects/project/shadow-mcp/server-slug?tab=users#top",
        ]}
      >
        <Routes>
          <Route
            path="/org/projects/project/shadow-mcp/:serverSlug"
            element={<ShadowMCPServerLegacyRedirect />}
          />
          <Route path="*" element={<LocationPath />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(
      await screen.findByText(
        "/org/projects/project/shadow-ai/mcps/server-slug?tab=users#top",
      ),
    ).toBeTruthy();
  });

  it("navigates to absolute routes through goTo", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <GoToExploreDemo />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByText("go"));

    expect(screen.getByText("/explore-demo")).toBeTruthy();
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

it("removes platform issuer management while preserving tenant and other admin routes", () => {
  expect(orgRoutePaths).not.toContain("platform-remote-identity-providers");
  expect(orgRoutePaths).toContain("remote-identity-providers/*");
  expect(orgRoutePaths).toContain("platform-admin");
  expect(orgRoutePaths).toContain("platform-admin/openrouter-keys");
});
