import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ScopeList } from "./ScopeList";

const GOOGLE = "https://www.googleapis.com/auth/";

// Chips are the list items that are not the "+N more" toggle.
const chips = () =>
  screen
    .getAllByRole("listitem")
    .filter((item) => !item.querySelector("button"))
    .map((item) => item.textContent);

describe("ScopeList", () => {
  afterEach(cleanup);

  it("shows a dash when there are no scopes", () => {
    render(<ScopeList scopes={null} />);
    expect(screen.getByText("—")).toBeTruthy();
  });

  it("shows every scope when clamped lines are not exceeded", () => {
    render(<ScopeList scopes={["read", "write"]} maxLines={3} />);
    expect(chips()).toEqual(["read", "write"]);
    expect(screen.queryByText(/more/)).toBeNull();
  });

  it("shows a short list whole, without a toggle", () => {
    render(<ScopeList scopes={["read", "write"]} />);
    expect(chips()).toEqual(["read", "write"]);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("collapses a long list behind a toggle", () => {
    const scopes = Array.from({ length: 30 }, (_, i) => `scope:${i}`);
    render(<ScopeList scopes={scopes} />);
    expect(chips()).toHaveLength(12);

    fireEvent.click(screen.getByRole("button", { name: "+18 more" }));
    expect(chips()).toHaveLength(30);
  });

  it("shows scopes under a shared URL base without it", () => {
    render(
      <ScopeList
        scopes={[
          "openid",
          `${GOOGLE}calendar`,
          `${GOOGLE}drive`,
          `${GOOGLE}gmail`,
        ]}
      />,
    );
    expect(chips()).toEqual(["openid", "calendar", "drive", "gmail"]);
    expect(screen.queryByText(`${GOOGLE}calendar`)).toBeNull();
  });

  it("shows a repeated scope once", () => {
    render(<ScopeList scopes={["read", "read", "write"]} />);
    expect(chips()).toEqual(["read", "write"]);
  });
});
