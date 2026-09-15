import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ICON_TARGET_IDS } from "./ai-tool-icon-sources";
import { AIToolIcon } from "./AIToolIcon";

describe("AIToolIcon", () => {
  it("renders the vendor mark for a tool Gram ships a target for", () => {
    const { container } = render(
      <AIToolIcon targetId="cursor" displayName="Cursor" />,
    );
    expect(container.querySelector("svg")).not.toBeNull();
    expect(screen.queryByText("C")).toBeNull();
  });

  // The whole reason this component exists instead of a bare
  // AgentProviderIcon: that matcher's includes("cursor") would put Cursor's
  // logo on an organization's own target, in the table people read to decide
  // what to block.
  it("never gives a custom target a vendor's logo by name", () => {
    render(<AIToolIcon targetId="cursor-fork" displayName="Cursor Fork" />);
    expect(screen.getByText("C")).toBeTruthy();
  });

  it("falls back to a monogram rather than a shared globe", () => {
    // Aider publishes only a 200x60 wordmark, which does not reduce to a
    // 16px square, so it stays on the monogram deliberately.
    render(<AIToolIcon targetId="aider" displayName="Aider" />);
    expect(screen.getByText("A")).toBeTruthy();
  });

  it("covers every scan target Gram ships a mark for", () => {
    // Walk the map itself: a hand-kept list of ids drifts from it, and a
    // vendor mark that stopped resolving would then go unnoticed.
    expect(ICON_TARGET_IDS.length).toBeGreaterThanOrEqual(17);
    for (const targetId of ICON_TARGET_IDS) {
      const { container, unmount } = render(
        <AIToolIcon targetId={targetId} displayName={targetId} />,
      );
      // An svg or an img, never the monogram tile.
      expect(
        container.querySelector("svg") ?? container.querySelector("img"),
        `${targetId} fell back to the monogram`,
      ).not.toBeNull();
      unmount();
    }
  });

  // Object.hasOwn guards the lookup: these are inherited Object properties,
  // and a custom target could be named after one of them.
  it("treats a target named after an Object property as unknown", () => {
    for (const targetId of ["constructor", "toString", "__proto__"]) {
      const { unmount } = render(
        <AIToolIcon targetId={targetId} displayName="Zed" />,
      );
      expect(screen.getByText("Z")).toBeTruthy();
      unmount();
    }
  });
});
