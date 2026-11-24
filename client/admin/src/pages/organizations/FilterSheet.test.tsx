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

it("opens directly on a custom endpoint and clearing an invalid bound recovers", async () => {
  const onApply = vi.fn<() => void>();
  render(
    <FilterSheet
      value={{
        ...NO_FILTERS,
        createdFrom: "2024-02-29",
        createdTo: "2024-03-01",
      }}
      openGroup="createdTo"
      onApply={onApply}
      onOpenChange={vi.fn<() => void>()}
      onReturnFocus={vi.fn<() => void>()}
    />,
  );
  const to = screen.getByRole("textbox", { name: "To (UTC)" });
  await waitFor(() => expect(document.activeElement).toBe(to));
  fireEvent.change(to, { target: { value: "2024-02-30" } });
  expect(
    (screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.change(to, { target: { value: "" } });
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  expect(onApply).toHaveBeenCalledWith({
    ...NO_FILTERS,
    minMembers: undefined,
    maxMembers: undefined,
    createdFrom: "2024-02-29",
    createdTo: undefined,
  });
});

it("offers the internal label as a single choice and applies it", async () => {
  const onApply = vi.fn<() => void>();
  render(
    <FilterSheet
      value={NO_FILTERS}
      openGroup="internal"
      onApply={onApply}
      onOpenChange={vi.fn<() => void>()}
      onReturnFocus={vi.fn<() => void>()}
    />,
  );
  const select = screen.getByRole("combobox", { name: /^Internal/ });
  expect(select.textContent).toBe("All organizations");
  select.focus();
  fireEvent.keyDown(select, { key: "ArrowDown" });
  expect(
    (await screen.findAllByRole("option")).map((option) => option.textContent),
  ).toEqual(["All organizations", "Internal only", "Exclude internal"]);
  fireEvent.click(screen.getByRole("option", { name: "Internal only" }));
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  expect(onApply).toHaveBeenCalledWith(
    expect.objectContaining({ internal: ["internal"] }),
  );
});
