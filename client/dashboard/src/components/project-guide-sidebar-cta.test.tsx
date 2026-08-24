import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const started = vi.hoisted(() => ({ current: false }));

vi.mock("@/components/project-guide/projectGuideStores", () => ({
  useProjectGuideStarted: () => started.current,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "project" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ guide: { href: () => "/org/projects/project/guide" } }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import { ProjectGuideSidebarCta } from "./project-guide-sidebar-cta";

afterEach(() => {
  cleanup();
  started.current = false;
});

describe("ProjectGuideSidebarCta", () => {
  it("links back to the guide after a journey has started", () => {
    started.current = true;
    render(<ProjectGuideSidebarCta />);

    expect(
      screen.getByRole("link", { name: /project guide/i }).getAttribute("href"),
    ).toBe("/org/projects/project/guide");
  });

  it("stays hidden until a journey has started", () => {
    render(<ProjectGuideSidebarCta />);

    expect(screen.queryByRole("link", { name: /project guide/i })).toBeNull();
  });
});
