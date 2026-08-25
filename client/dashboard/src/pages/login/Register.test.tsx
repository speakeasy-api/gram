import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import Register from "./Register";

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
function renderAt(path: string) {
  const [, search = ""] = path.split("?");
  mocks.useSearchParams.mockReturnValue([new URLSearchParams(search), vi.fn()]);
  render(<Register />);
}

let originalLocation: Location | undefined;

beforeEach(() => {
  originalLocation = window.location;
  // @ts-expect-error test-only location replacement for same-origin URL parsing
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { origin: "https://app.example" },
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

describe("Register", () => {
  it("redirects logged-out /register traffic to /sign-up", () => {
    mocks.useSession.mockReturnValue({ session: "", activeOrganizationId: "" });

    renderAt("/register?redirect=%2Fcli%2Fcallback");

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/sign-up?redirect=https%3A%2F%2Fapp.example%2Fcli%2Fcallback",
    );
  });

  it("preserves assistants disposition", () => {
    mocks.useSession.mockReturnValue({ session: "", activeOrganizationId: "" });

    renderAt("/register?disposition=assistants");

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/login?disposition=assistants",
    );
  });

  it("drops an external legacy redirect", () => {
    mocks.useSession.mockReturnValue({ session: "", activeOrganizationId: "" });

    renderAt("/register?redirect=https%3A%2F%2Fevil.example");

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/sign-up",
    );
  });

  it("redirects an authenticated session with an organization to the root", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });

    renderAt("/register");

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe("/");
    expect(screen.queryByTestId("register-panel")).toBeNull();
  });

  it("preserves the path, search, and hash for an authenticated redirect", () => {
    mocks.useSession.mockReturnValue({
      session: "<SESSION>",
      activeOrganizationId: "<ORG_ID>",
    });

    renderAt(
      "/register?redirect=https%3A%2F%2Fapp.example%2Fprojects%2Fdefault%3Ftab%3Dtools%23details",
    );

    expect(screen.getByTestId("navigate").getAttribute("data-to")).toBe(
      "/projects/default?tab=tools#details",
    );
  });
});
