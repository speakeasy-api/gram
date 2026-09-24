import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import SignUp from "./SignUp";

const mocks = vi.hoisted(() => ({
  useSearchParams: vi.fn(),
  useSession: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({ useSession: mocks.useSession }));
vi.mock("react-router", () => ({
  Navigate: ({ to }: { to: string }) => (
    <div data-testid="navigate" data-to={to} />
  ),
  useSearchParams: mocks.useSearchParams,
}));
vi.mock("./components/auth-shell", () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("./components/signup-panel", () => ({
  SignUpPanel: ({ redirectTo }: { redirectTo?: string | null }) => (
    <div data-testid="signup-panel" data-redirect-to={redirectTo} />
  ),
}));
vi.mock("./components/register-panel", () => ({
  RegisterPanel: ({ redirectTo }: { redirectTo?: string | null }) => (
    <div data-testid="register-panel" data-redirect-to={redirectTo} />
  ),
}));

let originalLocation: Location | undefined;

beforeEach(() => {
  mocks.useSearchParams.mockReturnValue([new URLSearchParams(), vi.fn()]);
  originalLocation = window.location;
  // @ts-expect-error test-only location replacement for redirect assertion
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      origin: "https://app.example",
    },
  });
});

afterEach(() => {
  cleanup();
  if (originalLocation) {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: originalLocation,
    });
  }
  vi.clearAllMocks();
});

describe("SignUp", () => {
  it("renders the existing signup form when logged out", () => {
    mocks.useSession.mockReturnValue({ session: "", activeOrganizationId: "" });

    render(<SignUp />);

    expect(screen.getByTestId("signup-panel")).toBeTruthy();
    expect(screen.queryByTestId("register-panel")).toBeNull();
  });

  it("passes the requested destination to the logged-out signup form", () => {
    mocks.useSession.mockReturnValue({ session: "", activeOrganizationId: "" });
    mocks.useSearchParams.mockReturnValue([
      new URLSearchParams("redirect=%2Fcli%2Fcallback"),
    ]);

    render(<SignUp />);

    expect(
      screen.getByTestId("signup-panel").getAttribute("data-redirect-to"),
    ).toBe("https://app.example/cli/callback");
  });

  it("renders organization registration for an authenticated zero-org session", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "",
    });
    mocks.useSearchParams.mockReturnValue([
      new URLSearchParams("redirect=%2Fcli%2Fcallback"),
    ]);

    render(<SignUp />);

    expect(
      screen.getByTestId("register-panel").getAttribute("data-redirect-to"),
    ).toBe("https://app.example/cli/callback");
  });

  it("redirects an authenticated session with an organization to the requested destination", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });
    mocks.useSearchParams.mockReturnValue([
      new URLSearchParams("redirect=%2Fprojects%2Fdefault"),
    ]);

    render(<SignUp />);

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/projects/default",
    );
  });

  it("preserves search and hash in the requested destination", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });
    mocks.useSearchParams.mockReturnValue([
      new URLSearchParams(
        "redirect=https%3A%2F%2Fapp.example%2Fprojects%2Fdefault%3Ftab%3Dtools%23details",
      ),
    ]);

    render(<SignUp />);

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/projects/default?tab=tools#details",
    );
  });

  it("redirects an authenticated session with an organization to the root", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });

    render(<SignUp />);

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe("/");
    expect(screen.queryByTestId("register-panel")).toBeNull();
  });
});
