import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MaskedMatch, RevealAllProvider, RevealAllToggle } from "./risk-ui";
const state = vi.hoisted(() => ({ restricted: false, mutate: vi.fn() }));
vi.mock("@/hooks/useSensitiveContentRestricted", () => ({
  useSensitiveContentRestricted: () => state.restricted,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));
function Controls() {
  return (
    <TooltipProvider>
      <RevealAllProvider>
        <RevealAllToggle />
        <MaskedMatch
          resultId="finding"
          matchRedacted="<redacted len=6 sha=abcdef12>"
        />
      </RevealAllProvider>
    </TooltipProvider>
  );
}
afterEach(cleanup);
beforeEach(() => {
  state.restricted = false;
  state.mutate.mockReset();
});
describe("sensitive evidence controls", () => {
  it("locks support access without issuing reveal-all requests", () => {
    state.restricted = true;
    render(<Controls />);
    expect(screen.queryByText("Click to reveal")).toBeNull();
    expect(
      screen
        .getByRole("button", { name: "Reveal all matches" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByLabelText("Evidence is hidden during support access."),
    ).toBeTruthy();
    expect(state.mutate).not.toHaveBeenCalled();
  });
  it("blocks automatic reveal-all after switching into support access", async () => {
    const view = render(<Controls />);
    fireEvent.click(screen.getByRole("button", { name: "Reveal all matches" }));
    expect(state.mutate).toHaveBeenCalledTimes(1);
    state.mutate.mockClear();
    state.restricted = true;
    view.rerender(<Controls />);
    expect(screen.queryByText("Click to reveal")).toBeNull();
    expect(state.mutate).not.toHaveBeenCalled();
    const toggle = screen.getByRole("button", { name: "Reveal all matches" });
    expect(toggle.hasAttribute("disabled")).toBe(true);
    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    expect(toggle.textContent).toBe("Reveal all");
    const trigger = toggle.parentElement!;
    expect(trigger.tabIndex).toBe(0);
    act(() => trigger.focus());
    expect(document.activeElement).toBe(trigger);
    expect((await screen.findByRole("tooltip")).textContent).toBe(
      "Evidence is hidden during support access.",
    );
  });
  it("keeps ordinary/demo reveals and hides cached plaintext on a restricted context switch", () => {
    state.mutate.mockImplementation((_args, options) =>
      options.onSuccess({ match: "secret" }),
    );
    const view = render(<Controls />);
    fireEvent.click(screen.getByText("Click to reveal"));
    expect(screen.getByText("secret")).toBeTruthy();
    state.restricted = true;
    view.rerender(<Controls />);
    expect(screen.queryByText("secret")).toBeNull();
    expect(state.mutate).toHaveBeenCalledTimes(1);
  });
});
