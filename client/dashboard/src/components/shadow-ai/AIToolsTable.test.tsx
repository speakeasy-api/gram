import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AIToolsTable } from "./AIToolsTable";

const mocks = vi.hoisted(() => ({
  useAiDetections: vi.fn(),
}));

vi.mock("@gram/client/react-query/aiDetections.js", () => ({
  useAiDetections: mocks.useAiDetections,
  invalidateAllAiDetections: vi.fn(),
}));

vi.mock("./AIToolDecisionSheet", () => ({
  AIToolDecisionSheet: () => null,
}));

// Opening a row navigates through the route helpers, which resolve the
// tenant slugs the same way the app does.
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

function LocationPath(): JSX.Element {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}</output>;
}

function detection(overrides: Partial<AIDetection> = {}): AIDetection {
  return {
    targetId: "cursor",
    displayName: "Cursor",
    category: "harness",
    userCount: 4,
    deviceCount: 6,
    signals: ["installed", "running"],
    versions: [],
    firstSeen: new Date("2026-09-01T00:00:00Z"),
    lastSeen: new Date("2026-09-10T00:00:00Z"),
    access: {
      state: "unreviewed",
      decision: "unreviewed",
      enforceable: false,
    },
    ...overrides,
  };
}

// The toolbar keeps its filters in the URL, so the table needs a router, and
// the status cell's tooltip needs the provider App mounts around the tree.
function renderTable(element: React.ReactElement, url = "/") {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <TooltipProvider>{element}</TooltipProvider>
    </MemoryRouter>,
  );
}

describe("AIToolsTable", () => {
  beforeEach(() => {
    mocks.useAiDetections.mockReturnValue({
      data: { detections: [detection()] },
      isLoading: false,
      error: null,
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  // The Local Models tab passes canDecide={false} because nothing about a local
  // model can be enforced, not because the admin reading it may see less.
  it("keeps the attribution columns on a tab where nothing can be decided", () => {
    renderTable(<AIToolsTable category="local_model" canDecide={false} />);

    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.getByText("Users")).toBeTruthy();
    expect(screen.getByText("Devices")).toBeTruthy();
  });

  it("shows the attribution columns to an organization admin", () => {
    renderTable(<AIToolsTable category="harness" canDecide />);

    expect(screen.getByText("Users")).toBeTruthy();
    expect(screen.getByText("Devices")).toBeTruthy();
  });

  // Opening a row answers "who runs this?" on the tool's own page, nested
  // under the tab. That holds on the Local Models tab too, where nothing can
  // be decided; the decision stays on the row's context menu elsewhere.
  it("opens a row onto the tool's users on every tab", () => {
    renderTable(
      <>
        <AIToolsTable category="local_model" canDecide={false} />
        <LocationPath />
      </>,
      "/org/projects/project/shadow-ai/models",
    );

    fireEvent.click(screen.getByText("Cursor"));

    expect(screen.getByTestId("location").textContent).toBe(
      "/org/projects/project/shadow-ai/models/cursor",
    );
  });

  // The filter lives in the URL. With nothing under the chosen status the
  // body would otherwise be an unlabeled blank row.
  it("names the status when a filter leaves nothing to show", () => {
    renderTable(
      <AIToolsTable category="harness" canDecide />,
      "/?status=blocked",
    );

    expect(screen.getByText("No blocked harnesses")).toBeTruthy();
    expect(screen.queryByText("Cursor")).toBeNull();
  });

  it("renders a tool Gram cannot recognise as plain unreviewed", () => {
    // What the server sends for a tool that publishes no CIMD document:
    // blocking is CIMD-only, so no decision about it can mean anything and
    // SummarizeAccess resolves it to unreviewed whatever is stored.
    mocks.useAiDetections.mockReturnValue({
      data: {
        detections: [
          detection({
            access: {
              state: "unreviewed",
              decision: "unreviewed",
              enforceable: false,
            },
          }),
        ],
      },
      isLoading: false,
      error: null,
    });

    renderTable(<AIToolsTable category="harness" canDecide />);

    // Three states and no fourth: no qualified badge that would tell an admin
    // a block is recorded but inert.
    expect(screen.getByText("Unreviewed")).toBeTruthy();
    expect(screen.queryByText("Blocked")).toBeNull();
  });

  it("leaves the enforceable case unqualified", () => {
    mocks.useAiDetections.mockReturnValue({
      data: {
        detections: [
          detection({
            targetId: "claude-code",
            displayName: "Claude Code",
            access: {
              state: "blocked",
              decision: "blocked",
              enforceable: true,
            },
          }),
        ],
      },
      isLoading: false,
      error: null,
    });

    renderTable(<AIToolsTable category="harness" canDecide />);

    expect(screen.getByText("Blocked")).toBeTruthy();
  });
});
