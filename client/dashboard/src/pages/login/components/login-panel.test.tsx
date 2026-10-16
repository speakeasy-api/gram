import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";

import { LoginPanel } from "./login-panel";

afterEach(() => {
  cleanup();
});

function renderPanel(initialEntry = "/login") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LoginPanel redirectTo={null} />
    </MemoryRouter>,
  );
}

describe("LoginPanel", () => {
  it("links to the sign-up page", () => {
    renderPanel();

    const link = screen.getByRole("link", {
      name: /sign up/i,
    });
    expect(link.getAttribute("href")).toBe("/sign-up");
    expect(screen.getByText(/don't have an account/i)).toBeTruthy();
  });
});

it("keeps the partner code and redirect when switching to signup after an error", () => {
  render(
    <MemoryRouter
      initialEntries={["/login?signin_error=init_error&partner_code=partner"]}
    >
      <LoginPanel redirectTo="/welcome" partnerCode="partner" />
    </MemoryRouter>,
  );
  const link = new URL(
    screen.getByRole("link", { name: /sign up/i }).getAttribute("href")!,
    window.location.origin,
  );
  expect(link.searchParams.get("partner_code")).toBe("partner");
  expect(link.searchParams.get("redirect")).toBe("/welcome");
  expect(screen.getByText(/Failed to initialize/)).toBeTruthy();
});
