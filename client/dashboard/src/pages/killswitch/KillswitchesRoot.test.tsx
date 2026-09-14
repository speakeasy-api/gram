import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  KillswitchIndexRedirect,
  KillswitchRecordRedirect,
} from "./KillswitchesRoot";

const mocks = vi.hoisted(() => ({
  detail: undefined as { userId: string } | undefined,
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
    canAccess: true,
    isLoading: false,
    reason: "allowed",
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ identities: { href: () => "/acme/p/identities" } }),
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

function renderAt(element: JSX.Element, at = "/acme/killswitch/ks-1") {
  return render(
    <MemoryRouter initialEntries={[at]}>
      <Routes>
        <Route path=":orgSlug/killswitch/:killswitchId" element={element} />
        <Route path="*" element={<Landed />} />
      </Routes>
    </MemoryRouter>,
  );
}

function Landed(): JSX.Element {
  const { pathname, search } = useLocation();
  return <output data-testid="landed">{`${pathname}${search}`}</output>;
}

afterEach(cleanup);
beforeEach(() => {
  mocks.detail = { userId: "user-1" };
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

  it("says where killswitches went rather than forwarding a reader who cannot open the directory", () => {
    mocks.canOpenDirectory = false;
    renderAt(<KillswitchIndexRedirect />);
    expect(screen.queryByTestId("landed")).toBeNull();
    expect(screen.queryByText("Killswitches moved")).not.toBeNull();
  });
});
