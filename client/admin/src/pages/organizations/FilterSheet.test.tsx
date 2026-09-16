import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NO_FILTERS } from "@/lib/organizationFilters";
import { FilterSheet } from "./FilterSheet";

afterEach(cleanup);
it("opens directly on Max and calls focus restoration on cancel", async () => {
  const apply = vi.fn();
  const close = vi.fn();
  const props = {
    value: { ...NO_FILTERS, minMembers: "0" },
    onApply: apply,
    onOpenChange: close,
    onReturnFocus: vi.fn(),
  };
  const { rerender } = render(
    <FilterSheet {...props} openGroup="maxMembers" />,
  );
  const max = screen.getByRole("textbox", { name: "Max" });
  await waitFor(() => expect(document.activeElement).toBe(max));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(close).toHaveBeenCalledWith(false);
  expect(apply).not.toHaveBeenCalled();
  rerender(<FilterSheet {...props} openGroup={null} />);
  await waitFor(() => expect(props.onReturnFocus).toHaveBeenCalled());
});
