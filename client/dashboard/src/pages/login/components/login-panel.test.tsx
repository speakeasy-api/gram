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
      name: /sign-up for a 14-day trial/i,
    });
    expect(link.getAttribute("href")).toBe("/sign-up");
    expect(screen.getByText(/no account/i)).toBeTruthy();
  });
});
