import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  sessionData: vi.fn(),
  group: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: undefined, projectSlug: undefined }),
  useIsPlatformAdminRef: () => ({ current: false }),
}));

// The route table pulls in every page; the provider only reads the org-level
// path list from it.
vi.mock("@/routes", () => ({ orgRoutePaths: [] }));

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

import { AuthProvider } from "./AuthProvider";
import { nullTelemetry, TelemetryStateProvider } from "./Telemetry";

const DAY = 24 * 60 * 60 * 1000;
const ORG = { id: "org-1", name: "Test Org", slug: "test-org", projects: [] };
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

function renderGate() {
  return render(
    <TelemetryStateProvider
      telemetry={telemetry}
      featureFlagsInitiallyAvailable
    >
      <MemoryRouter initialEntries={["/"]}>
        <AuthProvider>
          <div data-testid="app" />
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
});
