import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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
    for (const [targetId, name] of [
      ["ollama", "Ollama"],
      ["lmstudio", "LM Studio"],
      ["windsurf", "Windsurf"],
      ["hermes-agent", "Hermes Agent"],
    ] as const) {
      const { container, unmount } = render(
        <AIToolIcon targetId={targetId} displayName={name} />,
      );
      // An svg or an img, never the monogram tile.
      expect(
        container.querySelector("svg") ?? container.querySelector("img"),
      ).not.toBeNull();
      unmount();
    }
  });
});
