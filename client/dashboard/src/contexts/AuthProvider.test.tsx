import { MemoryRouter, useLocation } from "react-router";
import { TelemetryStateProvider, nullTelemetry } from "./Telemetry";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  type MockInstance,
  vi,
} from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";

import { AuthProvider } from "./AuthProvider";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useSession } from "./Auth";

const mocks = vi.hoisted(() => ({
  sessionData: vi.fn(),
  group: vi.fn(),
  switchScopes: vi.fn(),
}));

let replaceSpy: MockInstance | undefined;

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
    useSdkClient: () => ({
      auth: { switchScopes: mocks.switchScopes },
    }),
  };
});

// The route table pulls in every page; the provider only reads the org-level
// path list from it.
vi.mock("@/routes", () => ({
  orgRoutePaths: [
    "data",
    "data/event-feed",
    "data/exports",
    "setup",
    "setup/:taskSlug",
    "access-hub",
    "access-hub/:issuerId",
  ],
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

const DAY = 24 * 60 * 60 * 1000;
const PROJECT = { id: "project-1", name: "Default", slug: "default" };
const ORG = {
  id: "org-1",
  name: "Test Org",
  slug: "test-org",
  projects: [PROJECT],
};
const OTHER_PROJECT = {
  id: "project-2",
  name: "Other Project",
  slug: "other-project",
};
const OTHER_ORG = {
  id: "org-2",
  name: "Other Org",
  slug: "other-org",
  projects: [OTHER_PROJECT],
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

    // The expired gate lives on /trial-ended, so this render only redirects.
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

  it.each(["/setup", "/test-org/setup?task=enable-logging"])(
    "keeps pending setup session behind the loading gate at %s",
    (destination) => {
      mocks.sessionData.mockReturnValue({
        session: null,
        error: null,
        status: "pending",
      });

      renderGate(destination);

      expect(screen.getByRole("heading", { name: "Loading…" })).toBeTruthy();
      expect(screen.queryByTestId("app")).toBeNull();
      expect(screen.queryByTestId("session")).toBeNull();
      expect(screen.queryByTestId("book-demo")).toBeNull();
      expect(screen.queryByTestId("switch-org")).toBeNull();
      expect(screen.getByTestId("location").textContent).toBe(destination);
    },
  );

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

  it("preserves an org route with a dynamic segment over a same-named project", () => {
    const SETUP_PROJECT_ORG = {
      ...ORG,
      projects: [{ ...PROJECT, slug: "setup" }],
    };
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [SETUP_PROJECT_ORG],
        organization: SETUP_PROJECT_ORG,
        activeOrganizationId: SETUP_PROJECT_ORG.id,
        whitelisted: true,
      }),
    );

    renderGate("/test-org/setup/idp");

    expect(screen.getByTestId("location").textContent).toBe(
      "/test-org/setup/idp",
    );
  });

  it.each([
    "/test-org/access-hub",
    "/test-org/access-hub/11111111-1111-1111-1111-111111111111",
    "/test-org/access-hub/11111111-1111-1111-1111-111111111111?tab=machines#rules",
  ])("preserves Access Hub URL %s over a same-named project", (path) => {
    const ACCESS_HUB_PROJECT_ORG = {
      ...ORG,
      projects: [{ ...PROJECT, slug: "access-hub" }],
    };
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ACCESS_HUB_PROJECT_ORG],
        organization: ACCESS_HUB_PROJECT_ORG,
        activeOrganizationId: ACCESS_HUB_PROJECT_ORG.id,
        whitelisted: true,
      }),
    );

    renderGate(path);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(screen.getByTestId("location").textContent).toBe(path);
  });

  it("preserves a legacy project whose slug is agents", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [{ ...ORG, projects: [{ ...PROJECT, slug: "agents" }] }],
        organization: {
          ...ORG,
          projects: [{ ...PROJECT, slug: "agents" }],
        },
        whitelisted: true,
      }),
    );

    renderGate("/test-org/agents");

    expect(screen.getByTestId("location").textContent).toBe(
      "/test-org/projects/agents",
    );
  });
});

describe("AuthProvider cross-organization links", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    mocks.switchScopes.mockResolvedValue({});
  });

  afterEach(() => {
    cleanup();
    sessionStorage.clear();
    replaceSpy?.mockRestore();
    replaceSpy = undefined;
  });

  it("switches scope before opening a project link from another organization", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );
    replaceSpy = vi
      .spyOn(window.location, "replace")
      .mockImplementation(() => {});
    const destination =
      "/other-org/projects/other-project/mcp/x/server/settings?tab=auth#credentials";

    renderGate(destination);

    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalledWith({
        organizationId: OTHER_ORG.id,
      });
      expect(replaceSpy).toHaveBeenCalledWith(destination);
    });
    expect(screen.queryByTestId("app")).toBeNull();
  });

  it("does not switch scope for a stale legacy foreign project", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );

    renderGate("/other-org/missing-project/mcp");

    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(screen.getByTestId("location").textContent).toBe("/test-org");
  });

  it("switches scope for a valid legacy foreign project", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );
    replaceSpy = vi
      .spyOn(window.location, "replace")
      .mockImplementation(() => {});
    const destination = "/other-org/other-project/mcp";

    renderGate(destination);

    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalledWith({
        organizationId: OTHER_ORG.id,
      });
      expect(replaceSpy).toHaveBeenCalledWith(destination);
    });
  });

  it("does not combine the active organization with a stale foreign project", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );

    renderGate("/other-org/projects/missing-project/mcp");

    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(screen.getByTestId("location").textContent).toBe("/test-org");
  });

  it("rejects a protocol-relative destination before switching scope", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );

    renderGate("//other-org/projects/other-project/mcp");

    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(screen.getByTestId("location").textContent).toBe("/test-org");
  });

  it("falls back instead of repeating a scope switch after reload", () => {
    const destination = "/other-org/projects/other-project/mcp";
    sessionStorage.setItem(
      "organizationScopeSwitchAttempt",
      JSON.stringify({ organizationId: OTHER_ORG.id, destination }),
    );
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );

    renderGate(destination);

    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(screen.getByTestId("location").textContent).toBe("/test-org");
    expect(sessionStorage.getItem("organizationScopeSwitchAttempt")).toBeNull();
  });

  it("ignores a completed scope switch after the pending route unmounts", async () => {
    let resolveSwitch: (() => void) | undefined;
    mocks.switchScopes.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveSwitch = resolve;
      }),
    );
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );
    replaceSpy = vi
      .spyOn(window.location, "replace")
      .mockImplementation(() => {});

    const page = renderGate("/other-org/projects/other-project/mcp");
    await waitFor(() => expect(mocks.switchScopes).toHaveBeenCalledTimes(1));
    page.unmount();
    resolveSwitch?.();

    await waitFor(() => {
      expect(replaceSpy).not.toHaveBeenCalled();
      expect(
        sessionStorage.getItem("organizationScopeSwitchAttempt"),
      ).toBeNull();
    });
  });

  it("does not let an older rejection clear a newer switch marker", async () => {
    let rejectSwitch: ((error: Error) => void) | undefined;
    mocks.switchScopes.mockReturnValue(
      new Promise<void>((_, reject) => {
        rejectSwitch = reject;
      }),
    );
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );
    const oldDestination = "/other-org/projects/other-project/mcp";
    const newerAttempt = JSON.stringify({
      organizationId: OTHER_ORG.id,
      destination: "/other-org/projects/other-project/logs",
    });

    const page = renderGate(oldDestination);
    await waitFor(() => expect(mocks.switchScopes).toHaveBeenCalledTimes(1));
    sessionStorage.setItem("organizationScopeSwitchAttempt", newerAttempt);
    page.unmount();
    rejectSwitch?.(new Error("superseded request failed"));

    await waitFor(() => {
      expect(sessionStorage.getItem("organizationScopeSwitchAttempt")).toBe(
        newerAttempt,
      );
    });
  });

  it("shows an error and clears the retry marker when switching fails", async () => {
    mocks.switchScopes.mockRejectedValue(new Error("scope switch unavailable"));
    mocks.sessionData.mockReturnValue(
      gatedSession({
        organizations: [ORG, OTHER_ORG],
        whitelisted: true,
      }),
    );

    renderGate("/other-org/projects/other-project/mcp");

    await waitFor(() => {
      expect(screen.getByText("Something went wrong")).toBeTruthy();
    });
    expect(sessionStorage.getItem("organizationScopeSwitchAttempt")).toBeNull();
  });
});

// Portable "/@self" paths let external links (marketing CTAs, docs) deep-link
// into the app without knowing the visitor's org slug. They match no route,
// so AuthProvider must resolve them before route matching runs.
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

  // What auth.info returns for a user with no memberships: the server only
  // leaves activeOrganizationId empty when the organization list is empty too,
  // and the client falls back to a blank organization entry.
  function noOrgSession() {
    return portableSession({
      organizations: [],
      organization: { id: "", name: "", slug: "", projects: [] },
      activeOrganizationId: "",
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

  it("bounces a logged-out /@self visitor through login with the destination", () => {
    mocks.sessionData.mockReturnValue({
      session: null,
      error: new Error("unauthorized"),
      status: "error",
    });

    renderGate("/@self/settings?tab=members#top");

    expect(screen.getByTestId("location").textContent).toBe(
      "/login?redirect=%2F%40self%2Fsettings%3Ftab%3Dmembers%23top",
    );
  });

  it("sends a /@self session with no organization to sign-up", () => {
    mocks.sessionData.mockReturnValue(noOrgSession());

    renderGate("/@self/settings");

    expect(screen.getByTestId("location").textContent).toBe(
      "/sign-up?redirect=%2F%40self%2Fsettings",
    );
  });

  it("expands /@self into the active org", () => {
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/@self/settings?tab=members");

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/settings?tab=members",
    );
    expect(screen.getByTestId("app")).toBeTruthy();
  });

  it("resumes a /@self destination after login", () => {
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/login?redirect=%2F%40self%2Fsettings");

    expect(screen.getByTestId("location").textContent).toBe("/acme/settings");
  });

  it("keeps an explicit project instead of the last-visited one", () => {
    localStorage.setItem("preferredProject", "proj-b");
    mocks.sessionData.mockReturnValue(portableSession());

    renderGate("/@self/projects/default/toolsets");

    expect(screen.getByTestId("location").textContent).toBe(
      "/acme/projects/default/toolsets",
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

describe("AuthProvider organization host", () => {
  const ORG_HOST = "https://ai.example.test";
  const PAGE = "/test-org/mcp?tab=logs#recent";

  /**
   * The session transfer to the organization's host that lands on page. The
   * hash never goes into the server-visible transfer URL.
   */
  const transferTo = (page: string) =>
    `${ORG_HOST}/rpc/auth.transferIn?${new URLSearchParams({
      source_host: window.location.host,
      redirect: page.split("#")[0]!,
    }).toString()}`;

  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    // The move reads the browser's own location, not the router's.
    window.history.replaceState(null, "", PAGE);
    replaceSpy = vi
      .spyOn(window.location, "replace")
      .mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    replaceSpy?.mockRestore();
    replaceSpy = undefined;
  });

  it("hands the session to the organization's host with a transfer that keeps the path and query", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(PAGE);

    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledWith(transferTo(PAGE));
    });
    expect(replaceSpy).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("app")).toBeNull();
  });

  it("never moves an organization on the legacy host (no dashboard URL)", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: undefined,
      }),
    );

    renderGate(PAGE);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays when the organization's host is the current host", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: window.location.origin,
      }),
    );

    renderGate(PAGE);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays without an active organization", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationId: "",
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate("/login");

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays for an impersonation session", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        impersonatorEmail: "operator@example.test",
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(PAGE);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays when the cached session is no longer authorized", () => {
    mocks.sessionData.mockReturnValue({
      ...gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
      error: new GramError("unauthorized", {
        response: new Response(null, { status: 401 }),
        request: new Request("https://app.getgram.ai/rpc/auth.info"),
        body: "",
      }),
      status: "error",
    });

    renderGate(PAGE);

    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays when the session has no token", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        session: "",
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(PAGE);

    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("switches to the organization named in the URL instead of moving", async () => {
    mocks.switchScopes.mockResolvedValue({});
    const otherPage = "/other-org/projects/other-project/mcp";
    window.history.replaceState(null, "", otherPage);
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        organizations: [ORG, OTHER_ORG],
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(otherPage);

    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalledWith({
        organizationId: OTHER_ORG.id,
      });
    });
    expect(replaceSpy).not.toHaveBeenCalledWith(transferTo(otherPage));
  });

  it("stays when the dashboard URL is not absolute", () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: "/elsewhere",
      }),
    );

    renderGate(PAGE);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("stays on a hand-off page that keeps its token on this host", () => {
    const handoff = "/risk-policy-challenge/acknowledge";
    window.history.replaceState(null, "", handoff);
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(handoff);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("still moves another organization to a host this tab visited", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );
    renderGate(PAGE);
    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledTimes(1);
    });
    cleanup();

    const otherPage = "/other-org/mcp";
    window.history.replaceState(null, "", otherPage);
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        organizations: [ORG, OTHER_ORG],
        organization: OTHER_ORG,
        activeOrganizationId: OTHER_ORG.id,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );
    renderGate(otherPage);

    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledTimes(2);
    });
    expect(replaceSpy).toHaveBeenLastCalledWith(transferTo(otherPage));
  });

  it("records the move before leaving, so a failed transfer that comes straight back does not move again", async () => {
    // Source host: the organization lives on ORG_HOST.
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );
    let recordedBeforeLeaving = false;
    replaceSpy?.mockImplementation(() => {
      recordedBeforeLeaving = (
        sessionStorage.getItem("organizationHostMoveTimes") ?? ""
      ).includes("ai.example.test");
    });

    renderGate(PAGE);
    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledWith(transferTo(PAGE));
    });
    expect(recordedBeforeLeaving).toBe(true);
    cleanup();

    // The transfer failed (say the code expired) and the tab is back on the
    // source host with the same session: it stays and renders the app.
    renderGate(PAGE);
    expect(screen.getByTestId("app")).toBeTruthy();

    // Destination host: auth.info names no other host, so nothing moves.
    cleanup();
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: undefined,
      }),
    );
    renderGate(PAGE);
    expect(screen.getByTestId("app")).toBeTruthy();

    expect(replaceSpy).toHaveBeenCalledTimes(1);
  });

  it("does not move a tab that comes straight back to the same host", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );

    renderGate(PAGE);
    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledTimes(1);
    });
    cleanup();

    // The tab came back to this host: it renders the app instead of leaving.
    renderGate(PAGE);

    expect(screen.getByTestId("app")).toBeTruthy();
    expect(replaceSpy).toHaveBeenCalledTimes(1);
  });

  it("moves a tab that comes back to this host after the guard window", async () => {
    mocks.sessionData.mockReturnValue(
      gatedSession({
        whitelisted: true,
        activeOrganizationDashboardUrl: ORG_HOST,
      }),
    );
    const start = Date.now();
    const now = vi.spyOn(Date, "now").mockReturnValue(start);
    try {
      renderGate(PAGE);
      await waitFor(() => {
        expect(replaceSpy).toHaveBeenCalledTimes(1);
      });
      cleanup();

      // The person returns to the old host later in the same tab.
      now.mockReturnValue(start + 16_000);
      renderGate(PAGE);
      await waitFor(() => {
        expect(replaceSpy).toHaveBeenCalledTimes(2);
      });
      expect(replaceSpy).toHaveBeenLastCalledWith(transferTo(PAGE));
    } finally {
      now.mockRestore();
    }
  });
});

describe("AuthProvider server-rendered return targets", () => {
  const INSTALL_PAGE = "/mcp/linear/install?domain=custom";
  const LOGIN_WITH_INSTALL_PAGE = `/login?redirect=${encodeURIComponent(INSTALL_PAGE)}`;

  beforeEach(() => {
    vi.clearAllMocks();
    replaceSpy = vi
      .spyOn(window.location, "replace")
      .mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup();
    replaceSpy?.mockRestore();
    replaceSpy = undefined;
  });

  // /mcp/<slug>/install is rendered by the server, not the dashboard. Routing
  // to it client-side would make the provider read "mcp" as an org slug and
  // bounce the signed-in user to their org's home page.
  it("loads an install page return target from the server", async () => {
    mocks.sessionData.mockReturnValue(gatedSession({ whitelisted: true }));

    renderGate(LOGIN_WITH_INSTALL_PAGE);

    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalledWith(INSTALL_PAGE);
    });
    expect(replaceSpy).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("location").textContent).toBe(
      LOGIN_WITH_INSTALL_PAGE,
    );
    expect(screen.queryByTestId("app")).toBeNull();
  });

  it("still routes a dashboard return target client-side", () => {
    mocks.sessionData.mockReturnValue(gatedSession({ whitelisted: true }));

    renderGate("/login?redirect=%2Ftest-org%2Fsettings");

    expect(screen.getByTestId("location").textContent).toBe(
      "/test-org/settings",
    );
    expect(replaceSpy).not.toHaveBeenCalled();
  });
});
