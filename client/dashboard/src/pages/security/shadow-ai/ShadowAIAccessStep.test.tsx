import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { accessLibrary, detection, scanTarget } from "./access-fixtures";
import { type AccessDecision, buildAccessRows } from "./access-rows";
import { ShadowAIAccessStep } from "./ShadowAIAccessStep";

afterEach(cleanup);

function renderStep(
  draft: ReadonlyMap<string, AccessDecision> = new Map(),
  library = accessLibrary(),
) {
  const onDecide =
    vi.fn<(targetId: string, decision: AccessDecision) => void>();
  render(
    <TooltipProvider>
      <ShadowAIAccessStep
        rows={buildAccessRows(library, [detection()])}
        isPending={false}
        libraryFailed={false}
        detectionsFailed={false}
        draft={draft}
        onDecide={onDecide}
        disposition="allow_all"
      />
    </TooltipProvider>,
  );
  return { onDecide };
}

const control = (name: string) =>
  screen.getByRole("group", { name: `Access decision for ${name}` });

describe("ShadowAIAccessStep", () => {
  it("lists every library target, seen or not, with where it came from", () => {
    renderStep();

    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
    expect(screen.queryByText("4 · 6")).toBeNull();
    expect(screen.getAllByText("Action").length).toBeGreaterThan(0);
    expect(screen.queryByText("Decision")).toBeNull();
    expect(screen.getByText("Acme Tool")).toBeTruthy();
    expect(screen.getByText("Not seen yet")).toBeTruthy();
    expect(screen.queryByText("Built-in")).toBeNull();
    expect(screen.getAllByText("Custom")).toHaveLength(1);
  });

  it("offers no control for a target nothing could enforce a decision on", () => {
    renderStep();

    fireEvent.click(screen.getByText("Open models"));

    expect(screen.getByText("Ollama")).toBeTruthy();
    expect(screen.getByText("Inventory only")).toBeTruthy();
    expect(
      screen.queryByRole("group", { name: "Access decision for Ollama" }),
    ).toBeNull();
  });

  it("writes a press to the draft, and withdraws the option in force", () => {
    const { onDecide } = renderStep();

    fireEvent.click(within(control("Cursor")).getByText("Deny"));
    expect(onDecide).toHaveBeenCalledWith("cursor", "blocked");

    cleanup();
    const second = renderStep(new Map([["cursor", "blocked"]]));
    expect(
      within(control("Cursor")).getByText("Deny").getAttribute("aria-pressed"),
    ).toBe("true");
    fireEvent.click(within(control("Cursor")).getByText("Deny"));
    expect(second.onDecide).toHaveBeenCalledWith("cursor", "unreviewed");
  });

  it("shows a block on a removed tool and lets the admin clear it", () => {
    const { onDecide } = renderStep(new Map(), [
      scanTarget({
        id: "zed",
        displayName: "Zed",
        gatewayClient: {
          cimdVendorKeys: [],
          oauthClientIds: [],
          clientInfoNames: [],
        },
        access: {
          state: "blocked",
          decision: "blocked",
          enforceable: true,
          catalogStatus: "removed",
        },
      }),
    ]);

    expect(
      screen.getByText(
        "Removed from the Speakeasy catalog. This block still applies.",
      ),
    ).toBeTruthy();
    expect(
      within(control("Zed")).getByText("Deny").getAttribute("aria-pressed"),
    ).toBe("true");
    fireEvent.click(within(control("Zed")).getByText("Deny"));
    expect(onDecide).toHaveBeenCalledWith("zed", "unreviewed");
  });

  it("lists a tool of no known kind under Other, and hides Other when empty", () => {
    renderStep();
    expect(screen.queryByText("Other")).toBeNull();

    cleanup();
    renderStep(new Map(), [
      scanTarget({
        id: "gone-tool",
        displayName: "Gone Tool",
        category: unrecognized(""),
        access: {
          state: "blocked",
          decision: "blocked",
          enforceable: true,
          catalogStatus: "removed",
        },
      }),
    ]);
    expect(screen.getByText("Other")).toBeTruthy();
    expect(screen.getByText("Gone Tool")).toBeTruthy();
    expect(control("Gone Tool")).toBeTruthy();

    cleanup();
    renderStep(new Map(), [
      scanTarget({
        id: "claude",
        displayName: "Claude",
        category: unrecognized(""),
        access: {
          state: "blocked",
          decision: "blocked",
          enforceable: true,
          catalogStatus: "documents_disabled",
        },
      }),
    ]);
    expect(screen.getByText("Other")).toBeTruthy();
    expect(
      screen.getByText(
        "Sign-in documents are turned off for this client. This block still applies.",
      ),
    ).toBeTruthy();
    expect(control("Claude")).toBeTruthy();
  });
});
