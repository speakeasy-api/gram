import { cleanup, render, screen } from "@testing-library/react";
import { Wrench } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";

import { FullBleedBanner } from "./full-bleed-banner";

afterEach(cleanup);

function renderBanner(
  overrides: Partial<React.ComponentProps<typeof FullBleedBanner>> = {},
) {
  return render(
    <FullBleedBanner
      icon={Wrench}
      title="Finish setup"
      description="One line of supporting copy."
      actions={<button>Continue</button>}
      {...overrides}
    />,
  );
}

describe("FullBleedBanner", () => {
  it("renders the headline, copy and actions", () => {
    renderBanner();

    expect(screen.getByText("Finish setup")).toBeTruthy();
    expect(screen.getByText("One line of supporting copy.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Continue" })).toBeTruthy();
  });

  // Banners stack in one slot, so the frame is what keeps them reading as a
  // single column rather than a run of mismatched strips.
  it("spans the full width with the shared frame and gutters", () => {
    const { container } = renderBanner();

    const frame = container.firstElementChild!;
    expect(frame.className).toContain("w-full");
    expect(frame.className).toContain("border-b");
    expect(frame.firstElementChild!.className).toContain("max-w-7xl");
  });

  // Page furniture shouldn't be announced; a banner that reports a state
  // should. The caller decides which it is.
  it("carries no role by default", () => {
    renderBanner();

    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it.each(["alert", "status"] as const)("announces itself as %s", (role) => {
    renderBanner({ role });

    expect(screen.getByRole(role)).toBeTruthy();
  });

  // The onboarding banner names its frame for a view transition across route
  // changes, so both layers have to stay reachable from the caller.
  it("applies caller classes to the frame and its content row", () => {
    const { container } = renderBanner({
      className: "vt-frame",
      contentClassName: "vt-content",
    });

    const frame = container.firstElementChild!;
    expect(frame.className).toContain("vt-frame");
    expect(frame.firstElementChild!.className).toContain("vt-content");
  });

  it("lets the caller restyle the supporting copy", () => {
    renderBanner({ descriptionClassName: "hidden sm:line-clamp-2" });

    expect(
      screen.getByText("One line of supporting copy.").className,
    ).toContain("hidden");
  });
});
