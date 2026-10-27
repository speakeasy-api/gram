import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

const routeSlugs = vi.hoisted(() => ({
  orgSlug: "org",
  projectSlug: "project" as string | undefined,
}));
vi.mock("@/contexts/Sdk", () => ({ useSlugs: () => routeSlugs }));

import { LegacyDataRedirect } from "./pages/data-exports/LegacyDataRedirect";
import {
  ShadowAISectionRedirect,
  ShadowMCPIndexRedirect,
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

  it("keeps the Shadow AI and Shadow MCP section URLs routable", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <ProjectRouteHrefs />
      </MemoryRouter>,
    );

    const hrefs = screen.getByTestId("project-route-hrefs").textContent ?? "";
    expect(hrefs).toContain("/org/projects/project/shadow-ai");
    // Both sections live in Guardrails now, but their URLs are in tickets,
    // bookmarks and block messages, so they stay routable and redirect.
    expect(hrefs).toContain("/org/projects/project/shadow-mcp");
  });

  it("nests the detail pages under the old section tabs", () => {
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

  // Shadow AI lives in Guardrails: every tool index page of the old section
  // sends a pasted link there.
  it.each([
    "shadow-ai",
    "shadow-ai/harnesses",
    "shadow-ai/assistants",
    "shadow-ai/models",
  ])("redirects /%s to Guardrails", async (path) => {
    const view = render(
      <MemoryRouter initialEntries={[`/org/projects/project/${path}`]}>
        <Routes>
          <Route
            path={`/org/projects/project/${path}`}
            element={<ShadowAISectionRedirect />}
          />
          <Route path="*" element={<LocationPath />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(
      await within(view.container).findByText(
        "/org/projects/project/risk-policies",
      ),
    ).toBeTruthy();
  });

  // The MCPs index and the older Shadow MCP index were the approvals queue,
  // so a pasted link lands on the MCP approvals tab. Their query and fragment
  // named filters of the old page and are dropped rather than carried over.
  it.each(["shadow-ai/mcps", "shadow-mcp"])(
    "redirects /%s to the MCP approvals tab of Guardrails",
    async (path) => {
      const view = render(
        <MemoryRouter
          initialEntries={[
            `/org/projects/project/${path}?status=blocked#tools`,
          ]}
        >
          <Routes>
            <Route
              path={`/org/projects/project/${path}`}
              element={<ShadowMCPIndexRedirect />}
            />
            <Route path="*" element={<LocationPath />} />
          </Routes>
        </MemoryRouter>,
      );

      expect(
        await within(view.container).findByText(
          "/org/projects/project/risk-policies?tab=mcp-approvals",
        ),
      ).toBeTruthy();
    },
  );

  // Block messages and bookmarks carry the old server paths with a query and
  // a fragment; both must survive the redirect or the deep link lands at the
  // top.

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

function OrganizationConnectionsHref(): JSX.Element {
  const routes = useRoutes({ projectSlug: "default" });
  return (
    <output data-testid="org-connections">
      {routes.mcp.organizationGateway.href()}?tab=connections
    </output>
  );
}
it("builds organization connections from an org-only route using an explicit project", () => {
  routeSlugs.projectSlug = undefined;
  try {
    render(
      <MemoryRouter initialEntries={["/org/settings"]}>
        <OrganizationConnectionsHref />
      </MemoryRouter>,
    );
    expect(screen.getByTestId("org-connections").textContent).toBe(
      "/org/projects/default/mcp/organization-gateway?tab=connections",
    );
  } finally {
    routeSlugs.projectSlug = "project";
  }
});
