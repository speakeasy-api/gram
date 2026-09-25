import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { BrowserRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  KillswitchesRoot,
  KillswitchIndexRedirect,
  KillswitchRecordRedirect,
} from "./KillswitchesRoot";

const mocks = vi.hoisted(() => ({
  detail: undefined as
    | { principalKind: string; userId?: string; agentId?: string }
    | undefined,
  canAccess: true,
  isLoading: false,
  canOpenIdentity: true,
  canOpenDirectory: true,
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
// Answers only for project:read — the roster's own gate. A redirect that
// checked some other scope reads as unauthorized here rather than silently
// passing on a mock that grants everything.
vi.mock("@/hooks/useRBAC", () => {
  const granted = (scopes: string[]) =>
    mocks.canOpenDirectory && scopes.includes("project:read");
  return {
    useRBAC: () => ({
      hasScope: (scope: string) => granted([scope]),
      hasAnyScope: (scopes: string[]) => granted(scopes),
      hasAllScopes: (scopes: string[]) => granted(scopes),
      isLoading: false,
      grants: [],
      error: null,
    }),
  };
});
vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({
    canAccess: mocks.canAccess,
    isLoading: false,
    reason: "allowed",
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: { href: () => "/acme/p/identities" },
    fleet: { href: () => "/acme/p/fleet" },
  }),
}));
// Mirrors the real builder, which applies withIdentityWindow to every href —
// so the record forward is exercised against an href that already carries a
// query string rather than a bare path.
vi.mock("@/lib/useIdentityHref", async () => {
  const { useLocation: location } = await import("react-router");
  const { withIdentityWindow } = await import("@/lib/identity-urn");
  return {
    useIdentityHrefBuilder: () => {
      const { search } = location();
      return (ref: { userId?: string } | null) =>
        mocks.canOpenIdentity && ref?.userId
          ? withIdentityWindow(
              `/acme/p/identities/user%3A${ref.userId}/access`,
              search,
            )
          : null;
    },
  };
});
vi.mock("@gram/client/react-query/killswitch.js", () => ({
  useKillswitch: () => ({
    data: mocks.detail,
    isLoading: mocks.isLoading,
    error: null,
  }),
}));

vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => {
    throw new Error("Recovery must not query inventory");
  },
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  useKillswitchesInfinite: () => ({
    data: { pages: [{ result: { items: [] } }] },
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: () => ({ data: { servers: [] } }),
}));
vi.mock("@/components/killswitch/KillswitchRecord", () => ({
  KillswitchRecord: ({
    subjectAgent,
    onClose,
  }: {
    subjectAgent: { name: string };
    onClose: () => void;
  }) => (
    <section aria-label="Restriction">
      <h1>{subjectAgent.name}</h1>
      <button>Release restriction</button>
      <button onClick={onClose}>Close</button>
    </section>
  ),
}));

function renderAt(element: JSX.Element, at = "/acme/killswitch/ks-1") {
  window.history.replaceState(null, "", at);
  return render(
    <BrowserRouter>
      <Routes>
        <Route path=":orgSlug/killswitch/:killswitchId" element={element} />
        <Route path="*" element={<Landed />} />
      </Routes>
    </BrowserRouter>,
  );
}

function Landed(): JSX.Element {
  const { pathname, search } = useLocation();
  return <output data-testid="landed">{`${pathname}${search}`}</output>;
}

afterEach(cleanup);
beforeEach(() => {
  mocks.detail = { principalKind: "user", userId: "user-1" };
  mocks.canAccess = true;
  mocks.isLoading = false;
  mocks.canOpenIdentity = true;
  mocks.canOpenDirectory = true;
});

describe("retired killswitch addresses", () => {
  it("opens a killswitch on the access tab of the person it restricts", () => {
    renderAt(<KillswitchRecordRedirect />);
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/p/identities/user%3Auser-1/access?killswitch=ks-1",
    );
  });

  it("waits for the subject rather than guessing where to land", () => {
    mocks.isLoading = true;
    renderAt(<KillswitchRecordRedirect />);
    expect(screen.queryByTestId("landed")).toBeNull();
  });

  it("falls back to the directory when the subject cannot be resolved", () => {
    mocks.detail = undefined;
    renderAt(<KillswitchRecordRedirect />);
    expect(screen.getByTestId("landed").textContent).toBe("/acme/p/identities");
  });

  it("falls back to the directory for a reader who cannot open that person", () => {
    mocks.canOpenIdentity = false;
    renderAt(<KillswitchRecordRedirect />);
    expect(screen.getByTestId("landed").textContent).toBe("/acme/p/identities");
  });

  it("sends the retired roster to the people it would have listed", () => {
    renderAt(<KillswitchIndexRedirect />);
    expect(screen.getByTestId("landed").textContent).toBe("/acme/p/identities");
  });

  it("keeps the reader's window alongside the record it opens", () => {
    renderAt(
      <KillswitchRecordRedirect />,
      "/acme/killswitch/ks-1?range=custom&from=a&to=b&label=Last+quarter",
    );
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/p/identities/user%3Auser-1/access?range=custom&from=a&to=b&label=Last+quarter&killswitch=ks-1",
    );
  });

  it("carries the reader's window onto the directory", () => {
    mocks.detail = undefined;
    renderAt(<KillswitchRecordRedirect />, "/acme/killswitch/ks-1?range=7d");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/p/identities?range=7d",
    );
  });

  it("offers agent recovery without project read", () => {
    mocks.canOpenDirectory = false;
    renderAt(<KillswitchIndexRedirect />);
    expect(screen.queryByTestId("landed")).toBeNull();
    expect(
      screen.getByRole("region", { name: "Agent restrictions" }),
    ).toBeTruthy();
  });
});

describe("agent restriction recovery routes", () => {
  beforeEach(() => {
    mocks.detail = { principalKind: "agent", agentId: "agent-12345678" };
  });
  it("opens the exact agent restriction without inventory or the agent rollout", () => {
    renderAt(<KillswitchRecordRedirect />);
    expect(
      screen.getByRole("button", { name: "Release restriction" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Agent · agent-12" }),
    ).toBeTruthy();
    expect(screen.queryByText(/Deleted/)).toBeNull();
    expect(screen.queryByTestId("landed")).toBeNull();
  });
  it("returns to Fleet restrictions with project read", () => {
    renderAt(<KillswitchRecordRedirect />);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/p/fleet?tab=restrictions",
    );
  });
  it("returns to the killswitch index without project read", () => {
    mocks.canOpenDirectory = false;
    renderAt(<KillswitchRecordRedirect />);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByTestId("landed").textContent).toBe("/acme/killswitch");
  });
  it.each([
    { principalKind: "agent" },
    { principalKind: "unknown", agentId: "agent-1" },
  ])("refuses malformed target %j", (detail) => {
    mocks.detail = detail;
    renderAt(<KillswitchRecordRedirect />);
    expect(
      screen.getByText(
        /not an agent restriction|Unsupported restriction target/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Release restriction" }),
    ).toBeNull();
    expect(screen.queryByTestId("landed")).toBeNull();
  });
  it("retains the killswitch authorization gate", () => {
    mocks.canAccess = false;
    renderAt(<KillswitchesRoot />);
    expect(screen.getByText("Killswitch is not available")).toBeTruthy();
    expect(
      screen.queryByRole("region", { name: "Agent restrictions" }),
    ).toBeNull();
  });
});
