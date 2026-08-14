import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const projects = vi.hoisted(() => ({
  current: [{ id: "p1", name: "Alpha", slug: "alpha" }],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: projects.current }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSlugs: () => ({ orgSlug: "acme" }) }));
vi.mock("@/hooks/useOrgWelcomeBanner", () => ({
  useOrgWelcomeBanner: () => ({ visible: true }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ setup: { href: () => "/acme/setup" } }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import { OrgWelcomeBanner } from "./OrgWelcomeBanner";

const hrefFor = (cta: string) =>
  screen.getByText(cta).closest("a")?.getAttribute("href");

afterEach(() => {
  cleanup();
  localStorage.clear();
  projects.current = [{ id: "p1", name: "Alpha", slug: "alpha" }];
});

describe("OrgWelcomeBanner", () => {
  it("points each route card at its destination", () => {
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Enter demo org")).toBe("/explore-demo");
    expect(hrefFor("Start using Speakeasy")).toBe("/acme/projects/alpha");
    expect(hrefFor("Start setup wizard")).toBe("/acme/setup");
  });

  it("prefers the last-visited project, then default", () => {
    projects.current = [
      { id: "p1", name: "Alpha", slug: "alpha" },
      { id: "p2", name: "Default", slug: "default" },
    ];

    render(<OrgWelcomeBanner />);
    expect(hrefFor("Start using Speakeasy")).toBe("/acme/projects/default");

    cleanup();
    localStorage.setItem("preferredProject", "alpha");
    render(<OrgWelcomeBanner />);
    expect(hrefFor("Start using Speakeasy")).toBe("/acme/projects/alpha");
  });
});
