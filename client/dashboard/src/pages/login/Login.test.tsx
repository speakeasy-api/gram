import { cleanup, render, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import Login from "./Login";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  goHome: vi.fn(),
  useSession: vi.fn(),
  useSearchParams: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useSession: mocks.useSession }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ home: { goTo: mocks.goHome } }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => mocks.navigate,
  useSearchParams: mocks.useSearchParams,
}));
vi.mock("./components/auth-shell", () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("./components/login-panel", () => ({ LoginPanel: () => null }));

beforeEach(() => {
  mocks.useSearchParams.mockReturnValue([
    new URLSearchParams("partner_code=partner&redirect=%2Fwelcome"),
  ]);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("preserves partner signup intent for an authenticated zero-org session", async () => {
  mocks.useSession.mockReturnValue({
    session: "session",
    activeOrganizationId: "",
  });
  render(<Login />);
  await waitFor(() => expect(mocks.navigate).toHaveBeenCalled());
  const destination = new URL(
    mocks.navigate.mock.calls[0]![0],
    window.location.origin,
  );
  expect(destination.pathname).toBe("/sign-up");
  expect(destination.searchParams.get("partner_code")).toBe("partner");
  expect(destination.searchParams.get("redirect")).toBe("/welcome");
});

it("does not route existing organizations into partner registration", async () => {
  mocks.useSession.mockReturnValue({
    session: "session",
    activeOrganizationId: "existing-org",
  });
  render(<Login />);
  await waitFor(() =>
    expect(mocks.navigate).toHaveBeenCalledWith("/welcome", { replace: true }),
  );
});
