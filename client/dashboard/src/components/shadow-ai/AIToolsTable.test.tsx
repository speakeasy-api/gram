import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
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
function renderTable(element: React.ReactElement) {
  return render(
    <MemoryRouter>
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

  it("hides the attribution columns from a viewer who cannot decide", () => {
    renderTable(<AIToolsTable category="harness" canDecide={false} />);

    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.queryByText("Users")).toBeNull();
    expect(screen.queryByText("Devices")).toBeNull();
  });

  it("shows the attribution columns to an organization admin", () => {
    renderTable(<AIToolsTable category="harness" canDecide />);

    expect(screen.getByText("Users")).toBeTruthy();
    expect(screen.getByText("Devices")).toBeTruthy();
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
