import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { LegacyDataRedirect } from "./pages/data-exports/LegacyDataRedirect";
import AccessHubRedirect from "./pages/workload-identities/AccessHubRedirect";
import {
  ShadowMCPLegacyRedirect,
  ShadowMCPServerLegacyRedirect,
} from "./pages/shadow-ai/ShadowAI";
import { orgRoutePaths, useOrgRoutes, useRoutes } from "./routes";

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
        routes.shadowAI.harnesses.detail.href("cursor"),
        routes.shadowAI.mcps.href(),
        routes.shadowAI.mcps.detail.href("server-slug"),
      ].join("\n")}
    </output>
  );
}

function ClientSettingsHref(): JSX.Element {
  const routes = useRoutes();
  return (
    <output>
      {routes.remoteIdentityProviders.clientDetail.settings.href(
        "issuer-1",
        "client-1",
      )}
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

  it("nests the tabs and their detail pages under the section", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <ShadowAIHrefs />
      </MemoryRouter>,
    );

    expect(
      (screen.getByTestId("shadow-ai-hrefs").textContent ?? "").split("\n"),
    ).toEqual([
      "/org/projects/project/shadow-ai/harnesses",
      "/org/projects/project/shadow-ai/harnesses/cursor",
      "/org/projects/project/shadow-ai/mcps",
      "/org/projects/project/shadow-ai/mcps/server-slug",
    ]);
  });

  // RemoteSessionClientDetail redirects this retired tab to Overview.
  it("keeps the retired remote session client settings route", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <ClientSettingsHref />
      </MemoryRouter>,
    );

    expect(
      screen.getByText(
        "/org/projects/project/remote-identity-providers/issuer-1/clients/client-1/settings",
      ),
    ).toBeTruthy();
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

describe("Access Hub", () => {
  function AccessHubHrefs(): JSX.Element {
    const orgRoutes = useOrgRoutes();
    return (
      <output data-testid="access-hub-hrefs">
        {[
          orgRoutes.workloadIssuers.href(),
          orgRoutes.workloadIssuers.issuerDetail.href("issuer-1"),
        ].join("\n")}
      </output>
    );
  }

  it("lists its detail page among the organization route paths", () => {
    expect(orgRoutePaths).toContain("access-hub/:issuerId");
  });

  it("is an organization route with no project in its path", () => {
    expect(orgRoutePaths).toContain("access-hub");
    render(
      <MemoryRouter initialEntries={["/org"]}>
        <AccessHubHrefs />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("access-hub-hrefs").textContent).toBe(
      "/org/access-hub\n/org/access-hub/issuer-1",
    );
  });

  it.each([
    ["/org/projects/project/access-hub", "/org/access-hub"],
    [
      "/org/projects/project/access-hub/issuer-1?tab=machines#rules",
      "/org/access-hub/issuer-1?tab=machines#rules",
    ],
    ["/org/projects/project/workload-identities", "/org/access-hub"],
  ])("redirects the project URL %s", async (source, destination) => {
    const { container } = render(
      <MemoryRouter initialEntries={[source]}>
        <Routes>
          <Route
            path="/:orgSlug/projects/:projectSlug/access-hub/*"
            element={<AccessHubRedirect />}
          />
          <Route
            path="/:orgSlug/projects/:projectSlug/workload-identities"
            element={<AccessHubRedirect />}
          />
          <Route path="/:orgSlug/access-hub/*" element={<LocationPath />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await within(container).findByText(destination)).toBeTruthy();
  });
});

it("lists nested organization pages at every depth, under their parents", () => {
  expect(orgRoutePaths).toContain("signing-keys/:setId");
  expect(orgRoutePaths).toContain("signing-keys/:setId/overview");
  expect(orgRoutePaths).toContain("access/roles");
  expect(orgRoutePaths).not.toContain(":issuerId");
  expect(orgRoutePaths).not.toContain("");
});

it("removes platform issuer management while preserving tenant and other admin routes", () => {
  expect(orgRoutePaths).not.toContain("platform-remote-identity-providers");
  expect(orgRoutePaths).toContain("remote-identity-providers/*");
  expect(orgRoutePaths).toContain("platform-admin");
  expect(orgRoutePaths).toContain("platform-admin/openrouter-keys");
});
