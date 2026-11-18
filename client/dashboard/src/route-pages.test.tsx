import { renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { pageHrefs, withProjectPages } from "./route-pages";
import { useRoutes } from "./routes";

const routes = () =>
  renderHook(() => useRoutes(), { wrapper: MemoryRouter }).result.current;

describe("pageHrefs", () => {
  it("includes nested routes that render a page", () => {
    const hrefs = pageHrefs(Object.values(withProjectPages(routes())));

    expect(hrefs.has("/org/projects/project/identities/people")).toBe(true);
    expect(hrefs.has("/org/projects/project/identities/agents")).toBe(true);
    expect(hrefs.has("/org/projects/project/mcp")).toBe(true);
  });

  it("leaves out external links and routes behind a path parameter or splat", () => {
    const hrefs = pageHrefs(Object.values(withProjectPages(routes())));

    expect(hrefs.has("https://docs.getgram.ai")).toBe(false);
    expect([...hrefs].some((href) => /[:*]/.test(href))).toBe(false);
    expect(hrefs.has("/org/projects/project/costs")).toBe(true);
  });
});
