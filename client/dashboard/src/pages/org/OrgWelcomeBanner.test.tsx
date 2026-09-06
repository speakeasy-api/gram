import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { OrgWelcomeBanner } from "./OrgWelcomeBanner";

const announcementOn = vi.hoisted(() => ({ current: false }));
const TEST_ANNOUNCEMENT = vi.hoisted(() => ({
  id: "explore-demo",
  title: "Explore the demo org",
  body: "A read-only organization with simulated traffic.",
  cta: "Enter demo org",
  to: "/explore-demo",
}));

const projects = vi.hoisted(() => ({
  current: [{ id: "p1", name: "Alpha", slug: "alpha" }],
}));
const setupEligible = vi.hoisted(() => ({ current: true }));
const isAdmin = vi.hoisted(() => ({ current: true }));
const trial = vi.hoisted(() => ({
  current: null as { startedAt: Date; endsAt: Date } | null,
}));
const logsEnabled = vi.hoisted(() => ({ current: false }));
const overview = vi.hoisted(() => ({
  data: undefined as
    | { summary: { activeServersCount: number; totalToolCalls: number } }
    | undefined,
  isPending: false,
}));
const platformMcpEnabled = vi.hoisted(() => ({ current: true }));
const recordCta = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
  useOrganization: () => ({
    id: "org1",
    slug: "acme",
    projects: projects.current,
  }),
  useSession: () => ({ trial: trial.current }),
  useUser: () => ({ id: "user1" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme" }),
}));
vi.mock("@/hooks/useOnboardingCta", () => ({
  useOnboardingCta: () => ({ eligible: setupEligible.current }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "org:admin" ? isAdmin.current : true,
  }),
}));
vi.mock("@/hooks/useOrgWelcomeBanner", () => ({
  useOrgWelcomeBanner: () => ({ visible: true }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: {
      logsEnabled: logsEnabled.current,
      platformMcpEnabled: platformMcpEnabled.current,
    },
  }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock(
  "@gram/client/react-query/recordPlatformMCPDashboardCtaEvent.js",
  () => ({
    useRecordPlatformMCPDashboardCtaEventMutation: () => recordCta,
  }),
);
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({
    data: overview.data,
    isPending: overview.isPending,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    home: { href: () => "/acme" },
    setup: { href: () => "/acme/setup" },
    platformMcp: { href: () => "/acme/platform-mcp" },
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
vi.mock("@/components/ui/Button", () => ({
  Button: ({
    children,
    onClick,
    ...props
  }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" onClick={onClick} {...props}>
      {children}
    </button>
  ),
}));
vi.mock("./orgHomeAnnouncements", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./orgHomeAnnouncements")>();
  return {
    ...actual,
    activeOrgHomeAnnouncement: () =>
      announcementOn.current ? TEST_ANNOUNCEMENT : null,
  };
});

const hrefFor = (cta: string) =>
  screen.getByText(cta).closest("a")?.getAttribute("href");

const activeTrial = () => {
  const now = Date.now();
  return {
    startedAt: new Date(now - 24 * 60 * 60 * 1000),
    endsAt: new Date(now + 6 * 24 * 60 * 60 * 1000),
  };
};

const expiredTrial = () => {
  const now = Date.now();
  return {
    startedAt: new Date(now - 14 * 24 * 60 * 60 * 1000),
    endsAt: new Date(now - 24 * 60 * 60 * 1000),
  };
};

function withData() {
  logsEnabled.current = true;
  overview.isPending = false;
  overview.data = { summary: { activeServersCount: 2, totalToolCalls: 10 } };
}

afterEach(() => {
  cleanup();
  localStorage.clear();
  projects.current = [{ id: "p1", name: "Alpha", slug: "alpha" }];
  setupEligible.current = true;
  isAdmin.current = true;
  trial.current = null;
  logsEnabled.current = false;
  overview.data = undefined;
  overview.isPending = false;
  platformMcpEnabled.current = true;
  recordCta.mutate.mockReset();
  announcementOn.current = false;
});

describe("OrgWelcomeBanner", () => {
  it("paints the brand mesh on the hero", () => {
    const { container } = render(<OrgWelcomeBanner />);
    const section = container.querySelector("section");
    expect(section?.className).toContain("bg-gradient-to-br");
    expect(section?.className).toContain("from-card");
  });

  it("trial admin: demo, guide, enterprise — no announcement", () => {
    announcementOn.current = true;
    trial.current = activeTrial();
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Enter demo org")).toBe("/explore-demo");
    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(hrefFor("Begin rollout")).toBe("/acme/setup");
    expect(screen.queryByText("Announcement")).toBeNull();
    expect(
      screen.getByRole("heading", { name: /Choose your\s+first move/ }),
    ).toBeTruthy();
  });

  it("trial member: demo and guide only", () => {
    announcementOn.current = true;
    trial.current = activeTrial();
    isAdmin.current = false;
    setupEligible.current = false;
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Enter demo org")).toBe("/explore-demo");
    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(screen.queryByText("Begin rollout")).toBeNull();
    expect(screen.queryByText("Announcement")).toBeNull();
  });

  it("non-trial admin, zero data: guide and enterprise", () => {
    render(<OrgWelcomeBanner />);

    expect(screen.queryByText("Enter demo org")).toBeNull();
    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(hrefFor("Begin rollout")).toBe("/acme/setup");
    expect(screen.queryByText("Announcement")).toBeNull();
  });

  it("non-trial admin, has data: platform MCP and enterprise", () => {
    withData();
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Set up Platform MCP")).toBe(
      "/acme/platform-mcp?setup=1&entrySource=organization_home",
    );
    expect(hrefFor("Begin rollout")).toBe("/acme/setup");
    expect(screen.queryByText("Announcement")).toBeNull();
    expect(
      screen.getByRole("heading", { name: /Pick up where\s+you left off/ }),
    ).toBeTruthy();
  });

  it("substitutes guide when Platform MCP is off", () => {
    withData();
    platformMcpEnabled.current = false;
    render(<OrgWelcomeBanner />);

    expect(screen.queryByText("Set up Platform MCP")).toBeNull();
    expect(hrefFor("Open the guide")).toBe("/guide");
  });

  it("non-trial member, zero data: guide only", () => {
    isAdmin.current = false;
    setupEligible.current = false;
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(screen.queryByText("Begin rollout")).toBeNull();
    expect(screen.queryByText("Announcement")).toBeNull();
    expect(
      screen.getByRole("heading", { name: "Let’s get started" }),
    ).toBeTruthy();
  });

  it("non-trial member, has data: default project only", () => {
    isAdmin.current = false;
    setupEligible.current = false;
    withData();
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Open project")).toBe("/acme/projects/alpha");
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.queryByText("Announcement")).toBeNull();
  });

  it("treats an expired trial as non-trial", () => {
    trial.current = expiredTrial();
    render(<OrgWelcomeBanner />);

    expect(screen.queryByText("Enter demo org")).toBeNull();
    expect(hrefFor("Open the guide")).toBe("/guide");
  });

  it("shows a dismissible announcement when it is enabled", () => {
    announcementOn.current = true;
    isAdmin.current = false;
    setupEligible.current = false;
    render(<OrgWelcomeBanner />);

    expect(screen.getByText("Announcement")).toBeTruthy();
    expect(hrefFor(TEST_ANNOUNCEMENT.cta)).toBe("/explore-demo");
  });

  it("appends the announcement as a third card for a non-trial admin", () => {
    announcementOn.current = true;
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(hrefFor("Begin rollout")).toBe("/acme/setup");
    expect(screen.getByText("Announcement")).toBeTruthy();
  });

  it("records a Platform MCP impression and selection from org home", () => {
    withData();
    render(<OrgWelcomeBanner />);

    expect(recordCta.mutate).toHaveBeenCalledWith({
      request: {
        recordDashboardCtaEventRequestBody: {
          action: "impression",
          surface: "organization_home",
        },
      },
    });

    fireEvent.click(screen.getByText("Set up Platform MCP"));

    expect(recordCta.mutate).toHaveBeenCalledWith({
      request: {
        recordDashboardCtaEventRequestBody: {
          action: "selected",
          surface: "organization_home",
        },
      },
    });
  });

  it("records rollout intent when the setup card is clicked", () => {
    trial.current = activeTrial();
    render(<OrgWelcomeBanner />);

    expect(screen.getByText("8 steps · resumable")).toBeTruthy();
    fireEvent.click(screen.getByText("Begin rollout"));

    expect(localStorage.getItem("gram-org-welcome-rollout-started:acme")).toBe(
      "true",
    );
  });

  it("shows resume copy after rollout intent is recorded", () => {
    trial.current = activeTrial();
    localStorage.setItem("gram-org-welcome-rollout-started:acme", "true");
    render(<OrgWelcomeBanner />);

    expect(screen.getByText("Continue enterprise rollout")).toBeTruthy();
    expect(hrefFor("Resume rollout")).toBe("/acme/setup");
  });

  it("drops the setup card when the org cannot run the wizard", () => {
    trial.current = activeTrial();
    setupEligible.current = false;
    render(<OrgWelcomeBanner />);

    expect(screen.queryByText("Begin rollout")).toBeNull();
    expect(screen.getByText("Enter demo org")).toBeTruthy();
  });

  it("treats an org with no projects as zero data", () => {
    projects.current = [];
    render(<OrgWelcomeBanner />);

    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(
      (
        screen.queryByText("Begin rollout") ??
        screen.getByText("Resume rollout")
      )
        .closest("a")
        ?.getAttribute("href"),
    ).toBe("/acme/setup");
    expect(screen.queryByText("Open project")).toBeNull();
  });

  it("prefers the last-visited project, then default", () => {
    isAdmin.current = false;
    setupEligible.current = false;
    withData();
    projects.current = [
      { id: "p1", name: "Alpha", slug: "alpha" },
      { id: "p2", name: "Default", slug: "default" },
    ];

    render(<OrgWelcomeBanner />);
    expect(hrefFor("Open project")).toBe("/acme/projects/default");

    cleanup();
    localStorage.setItem("preferredProject", "alpha");
    render(<OrgWelcomeBanner />);
    expect(hrefFor("Open project")).toBe("/acme/projects/alpha");
  });

  it("dismisses the announcement without removing path cards", () => {
    announcementOn.current = true;
    isAdmin.current = false;
    setupEligible.current = false;
    const { container } = render(<OrgWelcomeBanner />);

    fireEvent.click(screen.getByLabelText("Dismiss announcement"));

    expect(screen.queryByText("Announcement")).toBeNull();
    expect(hrefFor("Open the guide")).toBe("/guide");
    expect(
      localStorage.getItem(
        `gram-org-home-announcement:acme:${TEST_ANNOUNCEMENT.id}`,
      ),
    ).toBe("true");
    const grid = [...container.querySelectorAll("div")].find((el) =>
      el.className.includes("lg:grid-cols-2"),
    );
    expect(grid).toBeTruthy();
  });

  it("keeps two cards on a 2-col grid and three on a 3-col grid", () => {
    isAdmin.current = false;
    setupEligible.current = false;
    const two = render(<OrgWelcomeBanner />);
    expect(
      [...two.container.querySelectorAll("div")].some((el) =>
        el.className.includes("lg:grid-cols-2"),
      ),
    ).toBe(true);

    two.unmount();
    trial.current = activeTrial();
    isAdmin.current = true;
    setupEligible.current = true;
    const three = render(<OrgWelcomeBanner />);
    expect(
      [...three.container.querySelectorAll("div")].some((el) =>
        el.className.includes("lg:grid-cols-3"),
      ),
    ).toBe(true);
  });
});
