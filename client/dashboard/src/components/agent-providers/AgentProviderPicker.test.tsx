import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { AgentProviderPicker } from "./AgentProviderPicker";

afterEach(cleanup);

const providers = [
  { id: "claude", name: "Claude Code", iconSource: "claude-code" },
  { id: "codex", name: "OpenAI Codex", iconSource: "codex" },
  { id: "gemini", name: "Gemini", iconSource: "gemini", available: false },
] as const;

function renderPicker(onValueChange = vi.fn<(id: string) => void>()) {
  render(
    <TooltipProvider>
      <AgentProviderPicker
        aria-label="AI tool"
        providers={[...providers]}
        value="claude"
        onValueChange={onValueChange}
      />
    </TooltipProvider>,
  );
  return onValueChange;
}

it("offers each tool as a radio with its logo, the chosen one checked", () => {
  renderPicker();
  const group = screen.getByRole("radiogroup", { name: "AI tool" });
  const claude = screen.getByRole("radio", { name: "Claude Code" });
  expect(group.contains(claude)).toBe(true);
  expect(claude.getAttribute("aria-checked")).toBe("true");
  expect(claude.querySelector("svg")).toBeTruthy();
  expect(
    screen
      .getByRole("radio", { name: "OpenAI Codex" })
      .getAttribute("aria-checked"),
  ).toBe("false");
});

it("reports the tool chosen", () => {
  const onValueChange = renderPicker();
  fireEvent.click(screen.getByRole("radio", { name: "OpenAI Codex" }));
  expect(onValueChange).toHaveBeenCalledWith("codex");
});

it("lists a tool that is coming soon without letting it be chosen", () => {
  const onValueChange = renderPicker();
  const gemini = screen.getByRole("radio", { name: /Gemini/ });
  expect(gemini.hasAttribute("disabled")).toBe(true);
  expect(gemini.textContent).toContain("Coming soon");
  fireEvent.click(gemini);
  expect(onValueChange).not.toHaveBeenCalled();
});
