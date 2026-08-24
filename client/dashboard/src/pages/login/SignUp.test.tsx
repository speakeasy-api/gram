import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import SignUp from "./SignUp";

const mocks = vi.hoisted(() => ({
  locationSetter: vi.fn(),
  useSearchParams: vi.fn(),
  useSession: vi.fn(),
  useRoutes: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({ useSession: mocks.useSession }));
vi.mock("@/routes", () => ({ useRoutes: mocks.useRoutes }));
vi.mock("react-router", () => ({
  useSearchParams: mocks.useSearchParams,
}));
vi.mock("./components/auth-shell", () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("./components/signup-panel", () => ({
  SignUpPanel: () => <div data-testid="signup-panel" />,
}));
vi.mock("./components/register-panel", () => ({
  RegisterPanel: ({ redirectTo }: { redirectTo?: string | null }) => (
    <div data-testid="register-panel" data-redirect-to={redirectTo} />
  ),
}));

let originalLocation: Location | undefined;

beforeEach(() => {
  mocks.locationSetter.mockReset();
  mocks.useSearchParams.mockReturnValue([new URLSearchParams(), vi.fn()]);
  mocks.useRoutes.mockReturnValue({ mcp: { goTo: vi.fn() } });
  originalLocation = window.location;
  // @ts-expect-error test-only location replacement for redirect assertion
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      origin: "https://app.example",
      set href(value: string) {
        mocks.locationSetter(value);
      },
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

  it("returns an authenticated session with an organization to the requested destination", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });
    mocks.useSearchParams.mockReturnValue([
      new URLSearchParams("redirect=%2Fprojects%2Fdefault"),
    ]);

    render(<SignUp />);

    expect(mocks.locationSetter).toHaveBeenCalledWith(
      "https://app.example/projects/default",
    );
  });
});
