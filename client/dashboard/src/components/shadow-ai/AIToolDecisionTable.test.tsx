import { TooltipProvider } from "@/components/ui/Tooltip";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AIToolDecisionTable } from "./AIToolDecisionTable";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@gram/client/react-query/setAIToolDecision.js", () => ({
  useSetAIToolDecisionMutation: () => ({
    mutate: mocks.mutate,
    isPending: mocks.isPending,
  }),
}));

vi.mock("@gram/client/react-query/aiDetections.js", () => ({
  invalidateAllAiDetections: vi.fn(),
}));

vi.mock("@gram/client/react-query/aiDetectionUsers.js", () => ({
  invalidateAllAiDetectionUsers: vi.fn(),
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
    access: { state: "unreviewed", decision: "unreviewed", enforceable: true },
    ...overrides,
  };
}

function renderTable(detections: AIDetection[]) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <AIToolDecisionTable
          detections={detections}
          isLoading={false}
          emptyState={<p>nothing here</p>}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

function decisionBody() {
  return mocks.mutate.mock.calls[0]?.[0]?.request.setAIToolDecisionRequestBody;
}

describe("AIToolDecisionTable", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.isPending = false;
  });

  it("carries the rationale on file with an inline decision", () => {
    renderTable([
      detection({
        access: {
          state: "allowed",
          decision: "approved",
          enforceable: true,
          rationale: "Approved by security review",
        },
      }),
    ]);

    fireEvent.click(screen.getByText("Disallow"));

    expect(decisionBody()).toEqual({
      targetId: "cursor",
      decision: "blocked",
      rationale: "Approved by security review",
    });
  });

  it("ignores presses while a decision is still saving", () => {
    mocks.isPending = true;
    renderTable([detection()]);

    fireEvent.click(screen.getByText("Approve"));

    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("marks a tool with no decision as unreviewed beside the control", () => {
    renderTable([detection()]);

    expect(screen.getByText("Unreviewed")).toBeTruthy();
    expect(screen.getByText("Approve").getAttribute("aria-pressed")).toBe(
      "false",
    );
    expect(screen.getByText("Disallow").getAttribute("aria-pressed")).toBe(
      "false",
    );
  });

  it("records the pressed decision for the tool", () => {
    renderTable([detection()]);

    fireEvent.click(screen.getByText("Disallow"));

    expect(decisionBody()).toEqual({ targetId: "cursor", decision: "blocked" });
  });

  it("withdraws a decision when its option is pressed again", () => {
    renderTable([
      detection({
        access: { state: "allowed", decision: "approved", enforceable: true },
      }),
    ]);

    expect(screen.getByText("Approve").getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(screen.queryByText("Unreviewed")).toBeNull();

    fireEvent.click(screen.getByText("Approve"));

    expect(decisionBody()).toEqual({
      targetId: "cursor",
      decision: "unreviewed",
    });
  });

  it("offers no control for a tool the gateway cannot recognize", () => {
    renderTable([
      detection({
        targetId: "ollama",
        displayName: "Ollama",
        category: "local_model",
        access: {
          state: "unreviewed",
          decision: "unreviewed",
          enforceable: false,
        },
      }),
    ]);

    expect(screen.getByText("Inventory only")).toBeTruthy();
    expect(screen.queryByText("Approve")).toBeNull();
  });

  it("shows the empty state when nothing was detected", () => {
    renderTable([]);

    expect(screen.getByText("nothing here")).toBeTruthy();
  });
});
