import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Operator } from "@gram/client/models/components/logfilter";
import { CustomFilterChip, FilterChip, SelectFilterChip } from "./FilterChip";
import { defineFilters } from "./filter-schema";

afterEach(cleanup);

const noopEdit = (): void => {};
const noopRemove = (): void => {};

const [server] = defineFilters([
  { id: "server", label: "Server", kind: "select" },
]);

const options = [
  { value: "a", label: "Alpha" },
  { value: "b", label: "Beta" },
];

// The bordered box around the body button, i.e. its padding and gaps.
function chipBox(button: HTMLElement): HTMLElement {
  return button.parentElement!;
}

describe("FilterChip", () => {
  it("treats a click on the chip's padding as a click on its body", async () => {
    const onClick = vi.fn<() => void>();
    const onRemove = vi.fn<() => void>();
    render(
      <FilterChip
        label="Alpha"
        ariaLabel="Server filter: Alpha"
        active
        onClick={onClick}
        onRemove={onRemove}
      />,
    );
    await userEvent.click(
      chipBox(screen.getByRole("button", { name: "Server filter: Alpha" })),
    );
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(onRemove).not.toHaveBeenCalled();
  });

  it("clears from the × without opening", async () => {
    const onClick = vi.fn<() => void>();
    const onRemove = vi.fn<() => void>();
    render(
      <FilterChip label="Alpha" active onClick={onClick} onRemove={onRemove} />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove Alpha filter" }),
    );
    expect(onRemove).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("opens from the keyboard on the body button", async () => {
    const onClick = vi.fn<() => void>();
    render(<FilterChip label="Alpha" ariaLabel="Body" onClick={onClick} />);
    screen.getByRole("button", { name: "Body" }).focus();
    await userEvent.keyboard("{Enter}");
    await userEvent.keyboard(" ");
    expect(onClick).toHaveBeenCalledTimes(2);
  });
});

describe("SelectFilterChip", () => {
  it("opens its menu from a click on the chip's padding", async () => {
    render(
      <SelectFilterChip
        dim={server!}
        value="a"
        options={options}
        onChange={vi.fn<(value: string | null) => void>()}
      />,
    );
    await userEvent.click(
      chipBox(screen.getByRole("button", { name: "Server filter: Alpha" })),
    );
    expect(await screen.findByPlaceholderText("Search server…")).toBeTruthy();
  });

  it("clears from the × without opening the menu", async () => {
    const onChange = vi.fn<(value: string | null) => void>();
    render(
      <SelectFilterChip
        dim={server!}
        value="a"
        options={options}
        onChange={onChange}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove Alpha filter" }),
    );
    expect(onChange).toHaveBeenCalledWith(null);
    expect(screen.queryByPlaceholderText("Search server…")).toBeNull();
  });
});

describe("CustomFilterChip", () => {
  const filter = {
    id: "f1",
    path: "http.status",
    op: Operator.Eq,
    value: "500",
  };

  it("opens its editor from a click on the chip's padding", async () => {
    render(
      <CustomFilterChip
        filter={filter}
        onEdit={noopEdit}
        onRemove={noopRemove}
      />,
    );
    await userEvent.click(
      chipBox(screen.getByRole("button", { name: "Edit filter http.status" })),
    );
    expect(await screen.findByPlaceholderText("value")).toBeTruthy();
  });

  it("removes from the × without opening the editor", async () => {
    const onRemove = vi.fn<(id: string) => void>();
    render(
      <CustomFilterChip
        filter={filter}
        onEdit={noopEdit}
        onRemove={onRemove}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Remove filter http.status" }),
    );
    expect(onRemove).toHaveBeenCalledWith("f1");
    expect(screen.queryByPlaceholderText("value")).toBeNull();
  });
});
