import { createRootRoute, createRoute } from "@tanstack/react-router";
import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AdminSessionInfo } from "@/lib/gramAdminApi";
import { renderRouteTree } from "@/test/harness";
import { AdminLayout } from "./AdminLayout";

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

// Exercise the outlet with a matched child instead of triggering a not-found
// warning from a bare root.
async function renderLayout(): Promise<HTMLElement> {
  const root = createRootRoute({ component: AdminLayout });
  const index = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <div>Layout content</div>,
  });
  await renderRouteTree(root.addChildren([index]));
  return screen.getByText("Layout content");
}

// happy-dom does not lay out the document, so these assertions only guard the
// structure and utility classes that configure the shell. They do not prove
// viewport geometry or that content actually scrolls.
describe("AdminLayout", () => {
  it("renders the outlet in a shell marked with a viewport-height class", async () => {
    stubSession();
    const content = await renderLayout();

    const wrapper = document.querySelector('[data-slot="sidebar-wrapper"]');
    const inset = screen.getByRole("main");
    expect(wrapper).not.toBeNull();
    expect(inset.parentElement).toBe(wrapper);
    expect(inset.contains(content)).toBe(true);
    // Stock shadcn only sets min-h-svh, a floor. h-svh is the ceiling.
    expect(wrapper!.classList.contains("h-svh")).toBe(true);
  });

  it("marks one overflow-auto region and a min-h-0 flex-1 chain above it", async () => {
    stubSession();
    const content = await renderLayout();

    const inset = screen.getByRole("main");
    const scrollRegion = content.parentElement;
    expect(scrollRegion).not.toBeNull();
    expect([...inset.querySelectorAll(".overflow-auto")]).toEqual([
      scrollRegion,
    ]);

    // Every link from the scroll region up to main has to opt out of a
    // content-sized minimum. One that does not leaves the chain below it
    // content-sized, and the viewport class on the shell stops mattering.
    for (
      let element: Element | null = scrollRegion;
      element && element !== inset;
      element = element.parentElement
    ) {
      expect(element.classList.contains("flex")).toBe(true);
      expect(element.classList.contains("min-h-0")).toBe(true);
      expect(element.classList.contains("flex-1")).toBe(true);
    }
  });
});
