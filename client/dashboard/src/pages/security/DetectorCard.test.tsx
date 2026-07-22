import { TooltipProvider } from "@/components/ui/tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DetectorCard } from "./DetectorCard";

afterEach(cleanup);

function renderCard({
  disabledReason,
  onToggle = vi.fn(),
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
        onCustomize={vi.fn()}
      />
    </TooltipProvider>,
  );
}

describe("DetectorCard", () => {
  it("keeps an enabled switch interactive and unwrapped", () => {
    const onToggle = vi.fn();
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
});
