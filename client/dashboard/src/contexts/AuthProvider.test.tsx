import { GramError } from "@gram/client/models/errors/gramerror.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  sessionData: vi.fn(),
  group: vi.fn(),
}));

// Slugs derived from the live router location, as the real hook derives them
// from the URL: the portable-path tests below navigate mid-render, and a
// frozen orgSlug would send the post-navigation render down the wrong gate.
vi.mock("@/contexts/Sdk", async () => {
  const { useLocation } = await import("react-router");
  return {
    useSlugs: () => {
      const parts = useLocation().pathname.split("/").filter(Boolean) as Array<
        string | undefined
      >;
      return {
        orgSlug: parts[0],
        projectSlug: parts[1] === "projects" ? parts[2] : undefined,
      };
    },
    useIsPlatformAdminRef: () => ({ current: false }),
  };
});

// The route table pulls in every page; the provider only reads the org-level
// path list from it.
vi.mock("@/routes", () => ({
  orgRoutePaths: ["data", "data/event-feed", "data/exports"],
}));

vi.mock("@/pages/demo/BookDemo", () => ({
  default: () => <div data-testid="book-demo" />,
}));

vi.mock("@/pages/demo/SwitchOrg", () => ({
  default: () => <div data-testid="switch-org" />,
}));

vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useSessionData: () => mocks.sessionData() as unknown,
}));

import { useSession } from "./Auth";
import { AuthProvider } from "./AuthProvider";
import { nullTelemetry, TelemetryStateProvider } from "./Telemetry";

const DAY = 24 * 60 * 60 * 1000;
const PROJECT = { id: "project-1", name: "Default", slug: "default" };
const ORG = {
  id: "org-1",
  name: "Test Org",
  slug: "test-org",
  projects: [PROJECT],
};
const OTHER_ORG = {
  id: "org-2",
  name: "Other Org",
  slug: "other-org",
  projects: [],
};

const telemetry = { ...nullTelemetry, group: mocks.group };

function gatedSession(overrides: Record<string, unknown> = {}) {
  return {
    session: {
      user: { id: "user-1", email: "user@example.test", isAdmin: false },
      session: "session-token",
      organizations: [ORG],
      organization: ORG,
      activeOrganizationId: ORG.id,
      whitelisted: false,
      trial: null,
      ...overrides,
    },
    error: null,
    status: "success",
  };
}

// Renders outside AuthProvider so it stays visible whichever gate wins,
// exposing where the provider's redirects finally settled.
const LocationProbe = () => {
  const location = useLocation();
  return (
    <div data-testid="location">
      {location.pathname + location.search + location.hash}
    </div>
  );
};

const SessionProbe = () => (
  <div data-testid="session">{useSession().session ?? "none"}</div>
);

function renderGate(initialPath: string | string[] = "/") {
  const initialEntries = Array.isArray(initialPath)
    ? initialPath
    : [initialPath];

  return render(
    <TelemetryStateProvider
      telemetry={telemetry}
      featureFlagsInitiallyAvailable
    >
      <MemoryRouter initialEntries={initialEntries}>
        <LocationProbe />
        <AuthProvider>
          <div data-testid="app" />
          <SessionProbe />
        </AuthProvider>
      </MemoryRouter>
    </TelemetryStateProvider>,
  );
}

const registeredOrgGroups = () =>
  mocks.group.mock.calls.filter((call) => call[0] === "organization");

// ProjectProvider — which normally registers the PostHog organization group —
// never mounts for a walled-off organization, so an organization-targeted flag
// would stay unresolved on exactly the pages that gate on one.
describe("AuthProvider organization telemetry group", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(cleanup);

  it("registers the organization before returning the cold-signup gate", () => {
    mocks.sessionData.mockReturnValue(gatedSession());

    renderGate();

    expect(screen.getByTestId("book-demo")).toBeTruthy();
    expect(screen.queryByTestId("app")).toBeNull();
    expect(registeredOrgGroups()).toEqual([["organization", ORG.slug, {}]]);
  });

  it("registers the organization before redirecting an expired trial", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        trial: {
          startedAt: new Date(Date.now() - 20 * DAY),
          endsAt: new Date(Date.now() - 6 * DAY),
        },
      }),
    );

    renderGate();

    // The expired gate lives on /talk-to-us, so this render only redirects.
    expect(screen.queryByTestId("book-demo")).toBeNull();
    expect(registeredOrgGroups()).toEqual([["organization", ORG.slug, {}]]);
  });

  it("registers the organization when the switcher takes precedence", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({ organizations: [ORG, OTHER_ORG] }),
    );

    renderGate();

    expect(screen.getByTestId("switch-org")).toBeTruthy();
    expect(registeredOrgGroups()).toEqual([["organization", ORG.slug, {}]]);
  });

  it("registers nothing while the session is still loading", () => {
    mocks.sessionData.mockReturnValue({
      session: null,
      error: null,
      status: "pending",
    });

    renderGate();

    expect(registeredOrgGroups()).toEqual([]);
  });

  it("retains cached authentication after a transient focus refetch error", () => {
    mocks.sessionData.mockReturnValue({
      ...gatedSession({ activeOrganizationId: undefined }),
      error: new Error("temporary network failure"),
      status: "error",
    });

    renderGate();

    expect(screen.getByTestId("session").textContent).toBe("session-token");
  });

  it("drops cached authentication after an unauthorized focus refetch", () => {
    const error = new GramError("unauthorized", {
      response: new Response(null, { status: 401 }),
      request: new Request("https://app.getgram.ai/rpc/auth.info"),
      body: "",
    });
    mocks.sessionData.mockReturnValue({
      ...gatedSession({ activeOrganizationId: undefined }),
      error,
      status: "error",
    });

    renderGate();

    expect(screen.getByTestId("session").textContent).toBe("");
  });

  it("lets authenticated users stay on /guide until the guide route resolves", () => {
    mocks.sessionData.mockReturnValue(gatedSession({ whitelisted: true }));

    renderGate(["/guide"]);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(screen.getByTestId("location").textContent).toBe("/guide");
  });
});

describe("AuthProvider legacy project redirects", () => {
  const DATA_PROJECT_ORG = {
    ...ORG,
    projects: [{ ...PROJECT, slug: "data" }],
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [DATA_PROJECT_ORG],
        organization: DATA_PROJECT_ORG,
        activeOrganizationId: DATA_PROJECT_ORG.id,
        whitelisted: true,
      }),
    );
  });

  afterEach(cleanup);

  it.each([
    "/test-org/data",
    "/test-org/data/event-feed",
    "/test-org/data/exports?status=enabled#latest",
  ])("preserves exact organization route %s", (path) => {
    renderGate(path);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(screen.getByTestId("location").textContent).toBe(path);
  });

  it("redirects an unknown Data subpath as a legacy project URL", () => {
    renderGate("/test-org/data/toolsets?status=enabled#latest");

    expect(screen.getByTestId("location").textContent).toBe(
      "/test-org/projects/data/toolsets?status=enabled#latest",
    );
  });
});

// Portable "/~" paths let external links (marketing CTAs, docs) deep-link
// into the app without knowing the visitor's org or project slugs. They match
// no route, so AuthProvider must resolve them before route matching runs.
describe("AuthProvider portable paths", () => {
  const PROJECT_ORG = {
    id: "org-3",
    name: "Acme",
    slug: "acme",
    projects: [{ slug: "proj-a" }, { slug: "proj-b" }],
  };

  function portableSession(overrides: Record<string, unknown> = {}) {
    return gatedSession({
      organizations: [PROJECT_ORG],
      organization: PROJECT_ORG,
      activeOrganizationId: PROJECT_ORG.id,
      whitelisted: true,
      ...overrides,
    });
  }

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  it("bounces a logged-out visitor through login with the destination", () => {
    mocks.sessionData.mockReturnValue({
      session: null,
      error: new Error("unauthorized"),
      status: "error",
    });

    renderGate("/~/toolsets?tab=all");

    expect(screen.getByTestId("location").textContent).toBe(
      "/login?redirect=%2F~%2Ftoolsets%3Ftab%3Dall",
    );
  });

  it("sends a session with no organization to sign-up with the destination", () => {
    mocks.sessionData.mockReturnValue(
      portableSession({ activeOrganizationId: "" }),
    );

    renderGate("/~/toolsets");

    expect(screen.getByTestId("location").textContent).toBe(
      "/sign-up?redirect=%2F~%2Ftoolsets",
    );
  });

  it("expands into the active org and first project", () => {
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/~/toolsets?tab=all");

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/projects/proj-a/toolsets?tab=all",
    );
    expect(screen.getByTestId("app")).toBeTruthy();
  });

  it("prefers the last-visited project", () => {
    localStorage.setItem("preferredProject", "proj-b");
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/~/toolsets");

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/projects/proj-b/toolsets",
    );
  });

  it("leaves ordinary paths alone", () => {
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/acme/projects/proj-a/toolsets");

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/projects/proj-a/toolsets",
    );
    expect(screen.getByTestId("app")).toBeTruthy();
  });
});
