import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";

import { SigninErrorNotice } from "./auth-errors";

afterEach(() => {
  cleanup();
});

function renderNotice(search: string) {
  return render(
    <MemoryRouter initialEntries={[`/login${search}`]}>
      <SigninErrorNotice />
    </MemoryRouter>,
  );
}

describe("SigninErrorNotice", () => {
  it.each([
    [
      "transfer_session_expired",
      "Your previous sign-in is no longer available. Please sign in again.",
    ],
    [
      "transfer_wrong_destination",
      "We couldn't complete sign-in on this site. Please sign in here.",
    ],
    [
      "transfer_not_transferable",
      "This session can't be moved to another site. Please sign in here.",
    ],
    [
      "transfer_expired",
      "This sign-in transfer is no longer valid. Please sign in again.",
    ],
    [
      "transfer_browser_mismatch",
      "We couldn't complete the sign-in transfer in this browser. Please sign in here.",
    ],
    [
      "transfer_access_changed",
      "Your access has changed. Please sign in again; contact your administrator if you still cannot get in.",
    ],
    [
      "transfer_temporary_error",
      "We couldn't complete sign-in right now. Please try again.",
    ],
  ])("explains the session transfer failure %s", (code, message) => {
    const params = new URLSearchParams({
      signin_error: code,
      redirect: "/acme/mcp?tab=logs",
    });
    renderNotice(`?${params.toString()}`);

    expect(screen.getByText(message)).toBeTruthy();
  });

  it("falls back to the generic message for an unknown code", () => {
    renderNotice("?signin_error=not_a_real_code");

    expect(
      screen.getByText(
        "Server error. Please try again later or contact support.",
      ),
    ).toBeTruthy();
  });

  it("shows nothing without a signin_error", () => {
    const { container } = renderNotice("?redirect=%2Facme");

    expect(container.textContent).toBe("");
  });
});
