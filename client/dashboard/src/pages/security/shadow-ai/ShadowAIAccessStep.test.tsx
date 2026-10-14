import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { accessLibrary, detection } from "./access-fixtures";
import { type AccessDecision, buildAccessRows } from "./access-rows";
import { ShadowAIAccessStep } from "./ShadowAIAccessStep";

afterEach(cleanup);

function renderStep(draft: ReadonlyMap<string, AccessDecision> = new Map()) {
  const onDecide =
    vi.fn<(targetId: string, decision: AccessDecision) => void>();
  render(
    <TooltipProvider>
      <ShadowAIAccessStep
        rows={buildAccessRows(accessLibrary(), [detection()])}
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
});
