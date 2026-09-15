import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// ?inline runs index.css through the Tailwind plugin and hands back the
// generated stylesheet. Reading declarations out of it, keyed by the classes an
// element actually carries, is what keeps this file off class strings: rename a
// utility and the assertions still hold, drop one and they fail. happy-dom
// cannot do this job with getComputedStyle, because it does not descend into
// Tailwind's `@layer` blocks and does not resolve `svh`.
import stylesheet from "@/index.css?inline";
import type { AdminSessionInfo } from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";
import { AdminLayout } from "./AdminLayout";

// A ceiling that resolves to the viewport, however it is spelled. `min-h-svh`
// is already on the wrapper, so a `max-height` clamp works as well as a
// `height`, and the test should not reject either.
const VIEWPORT_CEILING = /^(100svh|100dvh|100vh|100%)$/;
const NO_MINIMUM = /^0(px)?$/;

const SESSION: AdminSessionInfo = { email: "eng@example.test", name: "Eng" };

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// NavUser reads the session on mount, and happy-dom resolves a relative URL
// against localhost:3000, so without this the render leaves the process.
function stubSession(): void {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(SESSION)));
}

// Tailwind emits one flat rule per utility, so a body that admits no braces
// matches innermost rules only and never an `@layer` or `@media` wrapper.
const FLAT_RULE = /([^{}]+)\{([^{}]*)\}/g;

// Every declaration the stylesheet applies to an element through its own
// classes, in sheet order, so a later utility wins as it would in a browser.
function declarationsFor(element: Element): Map<string, string> {
  const classes = new Set(element.classList);
  const declarations = new Map<string, string>();

  for (const [, selector, body] of stylesheet.matchAll(FLAT_RULE)) {
    const match = /^\.([^\s,>+~:[\]]+)$/.exec(selector!.trim());
    // Tailwind escapes the characters a bare class cannot carry; undo that to
    // compare against the class list.
    if (!match || !classes.has(match[1]!.replaceAll("\\", ""))) continue;

    for (const declaration of body!.split(";")) {
      const colon = declaration.indexOf(":");
      if (colon === -1) continue;
      declarations.set(
        declaration.slice(0, colon).trim(),
        declaration.slice(colon + 1).trim(),
      );
    }
  }

  return declarations;
}

function scrollRegionWithin(root: Element): Element {
  const regions = [...root.querySelectorAll("*")].filter(
    (element) => declarationsFor(element).get("overflow") === "auto",
  );
  expect(regions).toHaveLength(1);
  return regions[0]!;
}

describe("AdminLayout", () => {
  it("caps the shell at the viewport", async () => {
    stubSession();
    await renderWithApp(<AdminLayout />);

    const wrapper = document.querySelector('[data-slot="sidebar-wrapper"]');
    expect(wrapper).not.toBeNull();
    expect(screen.getByRole("main").parentElement).toBe(wrapper);

    // Stock shadcn gives the wrapper `min-h-svh`, a floor with no ceiling, so a
    // tall page grows the shell instead of scrolling inside it.
    const declarations = declarationsFor(wrapper!);
    const ceiling =
      declarations.get("height") ?? declarations.get("max-height");
    expect(ceiling).toMatch(VIEWPORT_CEILING);
  });

  it("keeps one scroll region and lets every element above it shrink", async () => {
    stubSession();
    await renderWithApp(<AdminLayout />);

    const inset = screen.getByRole("main");
    const scrollRegion = scrollRegionWithin(inset);

    // Every link from the scroll region up to the inset has to give up its
    // content-based minimum, because one that does not makes the whole chain
    // below it content-sized and the ceiling above stops mattering.
    for (
      let element: Element | null = scrollRegion;
      element && element !== inset;
      element = element.parentElement
    ) {
      expect(declarationsFor(element).get("min-height")).toMatch(NO_MINIMUM);
      expect(declarationsFor(element).get("flex")).toBe("1");
    }
  });
});
