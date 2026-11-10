import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ScopeList } from "./ScopeList";

const GOOGLE = "https://www.googleapis.com/auth/";

// Chips are the list items that are not the "+N more" toggle, read as shown:
// a shortened chip also carries its full scope for screen readers.
const chips = () =>
  screen
    .getAllByRole("listitem")
    .filter((item) => !item.querySelector("button"))
    .map(
      (item) => (item.querySelector("[aria-hidden]") ?? item).textContent ?? "",
    );

describe("ScopeList", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows a dash when there are no scopes", () => {
    render(<ScopeList scopes={null} />);
    expect(screen.getByText("—")).toBeTruthy();
  });

  it("shows every scope when clamped lines are not exceeded", () => {
    render(<ScopeList scopes={["read", "write"]} maxLines={3} />);
    expect(chips()).toEqual(["read", "write"]);
    expect(screen.queryByText(/more/)).toBeNull();
  });

  it("clips past maxLines and counts the chips it hides", () => {
    // Lay chips out two to a 20px line; the list itself sits at the top.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(
      function (this: Element) {
        const index =
          this.tagName === "LI"
            ? Array.from(this.parentElement!.children).indexOf(this)
            : -1;
        const top = index < 0 ? 0 : Math.floor(index / 2) * 24;
        return new DOMRect(0, top, 100, index < 0 ? 0 : 20);
      },
    );
    const scopes = Array.from({ length: 10 }, (_, i) => `scope:${i}`);
    render(<ScopeList scopes={scopes} maxLines={3} />);

    expect(screen.getByText("and 4 more")).toBeTruthy();
    // The clipped chips leave the tab order and the accessibility tree.
    expect(
      screen
        .getAllByRole("listitem", { hidden: true })
        .map((item) => item.hasAttribute("inert")),
    ).toEqual([
      false,
      false,
      false,
      false,
      false,
      false,
      true,
      true,
      true,
      true,
    ]);
    expect(screen.getByRole("list").style.maxHeight).toBe("68px");
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
    // The full scope stays with its chip for screen readers and keyboards.
    const calendar = screen.getByText(`${GOOGLE}calendar`);
    expect(calendar.className).toContain("sr-only");
    expect(calendar.closest("[tabindex]")?.getAttribute("tabindex")).toBe("0");
    expect(screen.getByText("openid").closest("[tabindex]")).toBeNull();
  });

  it("labels a scope whole when its short form is another scope", () => {
    render(
      <ScopeList
        scopes={[
          "drive",
          `${GOOGLE}calendar`,
          `${GOOGLE}drive`,
          `${GOOGLE}gmail`,
        ]}
      />,
    );
    expect(chips()).toEqual(["drive", "calendar", `${GOOGLE}drive`, "gmail"]);
  });

  it("lets a whole scope that is cut off take focus", () => {
    // Only the long scope's text overflows its chip.
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockImplementation(
      function (this: HTMLElement) {
        return this.textContent === "custom:very-long-scope" ? 200 : 0;
      },
    );
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(100);
    render(<ScopeList scopes={["read", "custom:very-long-scope"]} />);

    expect(
      screen.getByText("custom:very-long-scope").closest("[tabindex]"),
    ).not.toBeNull();
    expect(screen.getByText("read").closest("[tabindex]")).toBeNull();
  });

  it("shows a repeated scope once", () => {
    render(<ScopeList scopes={["read", "read", "write"]} />);
    expect(chips()).toEqual(["read", "write"]);
  });
});
