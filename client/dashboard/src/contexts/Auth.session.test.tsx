import { act, cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { LoginCheck } from "@/components/app-layout";
import { sessionTokens } from "@/lib/session-token";
import { emptySession, SessionContext, useSessionData } from "./Auth";

// Keep auth.info cached and successful throughout. Refresh rejection must
// discard its stale header AND user metadata without depending on a refetch.
vi.mock("@gram/client/react-query/sessionInfo.js", () => ({
  useSessionInfo: () => ({
    status: "success",
    error: null,
    refetch: vi.fn(),
    data: {
      headers: { "gram-session": ["stale-cached-access"] },
      result: {
        activeOrganizationId: "test-org",
        gramAccountType: "enterprise",
        hasActiveSubscription: true,
        isAdmin: false,
        organizationOverride: false,
        organizations: [
          { id: "test-org", name: "Test", slug: "test", projects: [] },
        ],
        trial: null,
        userEmail: "test@example.invalid",
        userId: "test-user",
        whitelisted: true,
      },
    },
  }),
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function SessionRoutes() {
  const { session } = useSessionData();
  const value = session ?? emptySession;
  return (
    <SessionContext.Provider value={value}>
      <div data-testid="user">{value.user.email}</div>
      <div data-testid="token">{value.session}</div>
      <Routes>
        <Route path="/login" element={<div>Login screen</div>} />
        <Route element={<LoginCheck />}>
          <Route path="/private" element={<div>Authenticated screen</div>} />
        </Route>
      </Routes>
    </SessionContext.Provider>
  );
}

it("uses each new refresh header then redirects to login and removes cached identity on expiration", async () => {
  const fetcher = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(
      new Response(null, {
        status: 204,
        headers: { "Gram-Session": "access-1" },
      }),
    )
    .mockResolvedValueOnce(
      new Response(null, {
        status: 204,
        headers: { "Gram-Session": "access-2" },
      }),
    )
    .mockResolvedValueOnce(new Response(null, { status: 401 }));
  vi.stubGlobal("fetch", fetcher);
  await sessionTokens.refresh();
  render(
    <MemoryRouter initialEntries={["/private"]}>
      <SessionRoutes />
    </MemoryRouter>,
  );
  expect(screen.getByText("Authenticated screen")).toBeTruthy();
  expect(screen.getByTestId("token").textContent).toBe("access-1");
  expect(screen.getByTestId("user").textContent).toBe("test@example.invalid");

  // Reactivation rotates an access token even while the old one is live.
  await act(async () => {
    await sessionTokens.refresh(true);
  });
  expect(screen.getByTestId("token").textContent).toBe("access-2");
  await act(async () => {
    await sessionTokens.refresh(true);
  });
  expect(await screen.findByText("Login screen")).toBeTruthy();
  expect(screen.queryByText("Authenticated screen")).toBeNull();
  expect(screen.getByTestId("token").textContent).toBe("");
  expect(screen.getByTestId("user").textContent).toBe("");
});
