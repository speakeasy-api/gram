import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { OrgWelcomeBanner } from "./OrgWelcomeBanner";

const projects = vi.hoisted(() => ({
  current: [{ id: "p1", name: "Alpha", slug: "alpha" }],
}));
const setupEligible = vi.hoisted(() => ({ current: true }));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
  useOrganization: () => ({ id: "org1", projects: projects.current }),
  useUser: () => ({ id: "user1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme" }),
}));
vi.mock("@/hooks/useOnboardingCta", () => ({
  useOnboardingCta: () => ({ eligible: setupEligible.current }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/useOrgWelcomeBanner", () => ({
  useOrgWelcomeBanner: () => ({ visible: true }),
}));
// Resolves the same paths the real hooks build for org "acme".
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    home: { href: () => "/acme" },
    setup: { href: () => "/acme/setup" },
  }),
  useRoutes: ({ projectSlug }: { projectSlug?: string }) => ({
    exploreDemo: { href: () => "/explore-demo" },
    home: { href: () => `/acme/projects/${projectSlug}` },
    guide: { href: () => `/acme/projects/${projectSlug}/guide` },
  }),
}));
vi.mock("react-router", () => ({
  Link: ({
    to,
    children,
    onClick,
  }: {
    to: string;
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <a href={to} onClick={onClick}>
      {children}
    </a>
  ),
}));

const hrefFor = (cta: string) =>
  screen.getByText(cta).closest("a")?.getAttribute("href");

afterEach(() => {
  cleanup();
  localStorage.clear();
  projects.current = [{ id: "p1", name: "Alpha", slug: "alpha" }];
  setupEligible.current = true;
});

describe("OrgWelcomeBanner", () => {
  it("points each route card at its destination", () => {
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Enter demo org")).toBe("/explore-demo");
    expect(hrefFor("Start using Speakeasy")).toBe("/acme/projects/alpha/guide");
    expect(hrefFor("Begin rollout")).toBe("/acme/setup");
  });

  it("records rollout intent when the setup card is clicked", () => {
    render(<OrgWelcomeBanner />);

    fireEvent.click(screen.getByText("Begin rollout"));

    expect(localStorage.getItem("gram-org-welcome-rollout-started:acme")).toBe(
      "true",
    );
  });

  it("shows resume copy after rollout intent is recorded", () => {
    localStorage.setItem("gram-org-welcome-rollout-started:acme", "true");
    render(<OrgWelcomeBanner />);

    expect(screen.getByText("Continue enterprise rollout")).toBeTruthy();
    expect(hrefFor("Resume rollout")).toBe("/acme/setup");
  });

  it("drops the setup card when the org cannot run the wizard", () => {
    setupEligible.current = false;
    render(<OrgWelcomeBanner />);

    expect(screen.queryByText("Begin rollout")).toBeNull();
    expect(screen.getByText("Enter demo org")).toBeTruthy();
  });

  it("falls back to org home when the org has no projects", () => {
    projects.current = [];
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Start using Speakeasy")).toBe("/acme");
  });

  it("prefers the last-visited project, then default", () => {
    projects.current = [
      { id: "p1", name: "Alpha", slug: "alpha" },
      { id: "p2", name: "Default", slug: "default" },
    ];

    render(<OrgWelcomeBanner />);
    expect(hrefFor("Start using Speakeasy")).toBe(
      "/acme/projects/default/guide",
    );

    cleanup();
    localStorage.setItem("preferredProject", "alpha");
    render(<OrgWelcomeBanner />);
    expect(hrefFor("Start using Speakeasy")).toBe("/acme/projects/alpha/guide");
  });
});
