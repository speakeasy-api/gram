import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { ListAIDetectionUsersResult } from "@gram/client/models/components/listaidetectionusersresult.js";
import ShadowAIToolDetail from "./ShadowAIToolDetail";

const mocks = vi.hoisted(() => ({
  useAiDetectionUsers: vi.fn(),
  useParams: vi.fn(),
}));

vi.mock("react-router", () => ({
  useParams: mocks.useParams,
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
  // The user column links to the identity page, which reads the location to
  // carry the reader's window across.
  useLocation: () => ({
    pathname: "/org/projects/project/shadow-ai/harnesses/cursor",
    search: "",
  }),
  useNavigate: () => vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      detail: {
        overview: { href: (urn: string) => `/identities/${urn}/overview` },
      },
    },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
  useProjectSlugForRequests: () => "project",
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
    grants: [],
    error: null,
  }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }

  function Header({ children }: { children?: ReactNode }) {
    return <div>{children}</div>;
  }
  Header.Breadcrumbs = () => null;

  function Body({ children }: { children: ReactNode }) {
    return <main>{children}</main>;
  }

  function Section({ children }: { children: ReactNode }) {
    return <section>{children}</section>;
  }
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h1>{children}</h1>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.CTA = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
      Section,
    }),
  };
});

vi.mock("@/components/shadow-ai/AIToolDecisionSheet", () => ({
  AIToolDecisionSheet: ({
    detection,
    open,
  }: {
    detection: AIDetection | null;
    open: boolean;
  }) =>
    open && detection ? (
      <div data-testid="decision-sheet" data-target-id={detection.targetId} />
    ) : null,
}));

vi.mock("@gram/client/react-query/aiDetectionUsers.js", () => ({
  useAiDetectionUsers: mocks.useAiDetectionUsers,
  invalidateAllAiDetectionUsers: vi.fn(),
}));

function detection(overrides: Partial<AIDetection> = {}): AIDetection {
  return {
    targetId: "cursor",
    displayName: "Cursor",
    category: "harness",
    userCount: 2,
    deviceCount: 3,
    signals: ["installed", "running"],
    versions: ["1.7.49", "1.7.52"],
    firstSeen: new Date("2026-08-01T10:00:00Z"),
    lastSeen: new Date("2026-08-31T10:00:00Z"),
    access: {
      state: "unreviewed",
      decision: "unreviewed",
      enforceable: true,
    },
    ...overrides,
  };
}

function result(
  overrides: Partial<ListAIDetectionUsersResult> = {},
): ListAIDetectionUsersResult {
  return {
    detection: detection(),
    users: [
      {
        userEmail: "sam@example.com",
        deviceCount: 2,
        signals: ["installed", "running"],
        versions: ["1.7.49", "1.7.52"],
        firstSeen: new Date("2026-08-01T10:00:00Z"),
        lastSeen: new Date("2026-08-31T10:00:00Z"),
      },
      {
        userEmail: "alex@example.com",
        deviceCount: 1,
        signals: ["installed"],
        versions: [],
        firstSeen: new Date("2026-08-10T10:00:00Z"),
        lastSeen: new Date("2026-08-20T10:00:00Z"),
      },
    ],
    ...overrides,
  };
}

function loaded(data: ListAIDetectionUsersResult) {
  return { data, error: null, isError: false, isPending: false };
}

describe("ShadowAIToolDetail", () => {
  beforeEach(() => {
    mocks.useParams.mockReturnValue({ targetId: "cursor" });
    mocks.useAiDetectionUsers.mockReturnValue(loaded(result()));
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("lists every person the tool was detected for, linked to their identity page", () => {
    render(<ShadowAIToolDetail />);

    expect(mocks.useAiDetectionUsers).toHaveBeenCalledWith(
      { targetId: "cursor" },
      undefined,
      { enabled: true, throwOnError: false },
    );
    expect(screen.getByRole("heading", { name: "Cursor" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "User" })).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "sam@example.com" })
        .getAttribute("href"),
    ).toBe("/identities/email%3Asam%40example.com/overview");
    // The identity page's own evidence columns, per person instead of per tool.
    expect(screen.getByText("2 devices")).toBeTruthy();
    expect(screen.getByText("1 device")).toBeTruthy();
    expect(screen.getByText("Running")).toBeTruthy();
    expect(screen.getAllByText("Installed")).toHaveLength(2);
    expect(screen.getByText("1.7.49 · 1.7.52")).toBeTruthy();
  });

  // Opening a row no longer opens the decision form, so the page carries it.
  it("offers the access decision for a tool that speaks MCP", () => {
    render(<ShadowAIToolDetail />);

    fireEvent.click(screen.getByRole("button", { name: "Decide access" }));

    expect(
      screen.getByTestId("decision-sheet").getAttribute("data-target-id"),
    ).toBe("cursor");
  });

  // A local model never reaches the gateway, so there is no decision to
  // offer: the same reason the Local Models tab records none.
  it("offers no decision for a local model", () => {
    mocks.useAiDetectionUsers.mockReturnValue(
      loaded(result({ detection: detection({ category: "local_model" }) })),
    );

    render(<ShadowAIToolDetail />);

    expect(screen.getByRole("heading", { name: "Cursor" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Decide access" })).toBeNull();
  });

  it("says when the detections could not be loaded", () => {
    mocks.useAiDetectionUsers.mockReturnValue({
      data: undefined,
      error: new Error("boom"),
      isError: true,
      isPending: false,
    });

    render(<ShadowAIToolDetail />);

    expect(
      screen.getByText("Unable to load Shadow AI detections"),
    ).toBeTruthy();
    // With no row to name it by, the page falls back to the id in the URL.
    expect(screen.getByRole("heading", { name: "cursor" })).toBeTruthy();
  });
});
