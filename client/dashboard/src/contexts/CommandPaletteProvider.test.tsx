import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { CSSProperties } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useCommandPalette } from "./CommandPalette";
import { CommandPaletteProvider } from "./CommandPaletteProvider";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function PaletteControls({ hiddenStyle }: { hiddenStyle: CSSProperties }) {
  const { open, close } = useCommandPalette();

  return (
    <>
      <div style={hiddenStyle}>
        <button type="button" onClick={open}>
          Hidden opener
        </button>
      </div>
      <button type="button" data-slot="command-palette-trigger">
        Visible trigger
      </button>
      <button type="button" onClick={close}>
        Close palette
      </button>
    </>
  );
}

describe("CommandPaletteProvider focus restoration", () => {
  const hiddenAncestorStyles = [
    ["opacity", { opacity: 0 }],
    ["content visibility", { contentVisibility: "hidden" }],
  ] satisfies ReadonlyArray<readonly [string, CSSProperties]>;

  it.each(hiddenAncestorStyles)(
    "uses the visible fallback for an opener hidden by ancestor %s",
    (_, hiddenStyle) => {
      vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue([
        {} as DOMRect,
      ] as unknown as DOMRectList);
      vi.spyOn(window, "requestAnimationFrame").mockImplementation(
        (callback) => {
          callback(0);
          return 1;
        },
      );

      render(
        <CommandPaletteProvider>
          <PaletteControls hiddenStyle={hiddenStyle} />
        </CommandPaletteProvider>,
      );

      const opener = screen.getByRole("button", { name: "Hidden opener" });
      const fallback = screen.getByRole("button", { name: "Visible trigger" });
      opener.focus();
      fireEvent.click(opener);
      fireEvent.click(screen.getByRole("button", { name: "Close palette" }));

      expect(document.activeElement).toBe(fallback);
    },
  );
});
