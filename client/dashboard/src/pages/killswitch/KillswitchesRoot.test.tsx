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
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
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
vi.mock("@/lib/useIdentityHref", () => ({
  useIdentityHrefBuilder: () => (ref: { userId?: string } | null) =>
    mocks.canOpenIdentity && ref?.userId
      ? `/acme/p/identities/user%3A${ref.userId}/access`
      : null,
}));
vi.mock("@gram/client/react-query/killswitch.js", () => ({
  useKillswitch: () => ({
    data: mocks.detail,
    isLoading: mocks.isLoading,
    error: null,
  }),
}));

function renderAt(element: JSX.Element) {
  return render(
    <MemoryRouter initialEntries={["/acme/killswitch/ks-1"]}>
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
});
