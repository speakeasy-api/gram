import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DetectorCard } from "./DetectorCard";
import type { DetectorMode } from "./policy-data";

afterEach(cleanup);

function renderPiiCard(mode: DetectorMode) {
  render(
    <TooltipProvider>
      <DetectorCard
        category="pii"
        selected
        disabledRules={new Set()}
        mode={mode}
        onToggle={vi.fn<(checked: boolean) => void>()}
        onCustomize={vi.fn<() => void>()}
      />
    </TooltipProvider>,
  );
}

function renderCard({
  disabledReason,
  onToggle = vi.fn<(checked: boolean) => void>(),
}: {
  disabledReason?: string;
  onToggle?: (checked: boolean) => void;
} = {}) {
  render(
    <TooltipProvider>
      <DetectorCard
        category="shadow_mcp"
        selected={false}
        disabledRules={new Set()}
        disabledReason={disabledReason}
        onToggle={onToggle}
        onCustomize={vi.fn<() => void>()}
      />
    </TooltipProvider>,
  );
}

describe("DetectorCard", () => {
  it("keeps an enabled switch interactive and unwrapped", () => {
    const onToggle = vi.fn<(checked: boolean) => void>();
    renderCard({ onToggle });

    const toggle = screen.getByRole("switch", {
      name: "Shadow MCP built-in rule",
    }) as HTMLButtonElement;
    expect(toggle.disabled).toBe(false);
    expect(toggle.closest('[data-slot="tooltip-trigger"]')).toBeNull();

    fireEvent.click(toggle);
    expect(onToggle).toHaveBeenCalledWith(true);
  });

  it("shows a disabled switch reason only when its tooltip trigger is hovered", async () => {
    const reason = "Turn off other built-in rules to select Shadow MCP.";
    renderCard({ disabledReason: reason });

    const toggle = screen.getByRole("switch", {
      name: "Shadow MCP built-in rule",
    }) as HTMLButtonElement;
    const trigger = toggle.closest<HTMLElement>(
      '[data-slot="tooltip-trigger"]',
    );

    expect(toggle.disabled).toBe(true);
    expect(trigger).not.toBeNull();
    expect(trigger?.tabIndex).toBe(0);
    expect(trigger?.getAttribute("aria-label")).toBe("Shadow MCP unavailable");
    expect(trigger?.children.length).toBe(1);
    expect(trigger?.firstElementChild).toBe(toggle);

    const card = trigger?.parentElement;
    expect(card).not.toBeNull();

    fireEvent.pointerMove(card!, { pointerType: "mouse" });
    expect(screen.queryByRole("tooltip")).toBeNull();

    fireEvent.pointerMove(trigger!, { pointerType: "mouse" });
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(reason);
  });

  it("shows a disabled switch reason when its tooltip trigger is focused", async () => {
    const reason = "Turn off other built-in rules to select Shadow MCP.";
    renderCard({ disabledReason: reason });

    const toggle = screen.getByRole("switch", {
      name: "Shadow MCP built-in rule",
    });
    const trigger = toggle.closest<HTMLElement>(
      '[data-slot="tooltip-trigger"]',
    );

    expect(trigger).not.toBeNull();

    fireEvent.focus(trigger!);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(reason);
  });

  it("offers rule customization for PII under the presidio engine", () => {
    renderPiiCard("presidio");

    expect(
      screen.getByRole("switch", {
        name: "Personal Identifiable Information built-in rule",
      }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Customize" })).toBeTruthy();
    expect(screen.getByText(/\d+ rules/)).toBeTruthy();
  });

  it("renders PII as a category-level detector under the LLM analyzer", () => {
    renderPiiCard("llm");

    expect(
      screen.getByRole("switch", { name: "PII built-in rule" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Personal data about an identifiable person; the category is decided per finding.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Customize" })).toBeNull();
    expect(screen.queryByText(/\d+ rules/)).toBeNull();
  });
});
