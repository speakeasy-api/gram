import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineFilters } from "./filter-schema";
import { FilterControl } from "./FilterControl";

afterEach(cleanup);

const [optional, required, named] = defineFilters([
  { id: "optional", label: "Signal", kind: "select" },
  { id: "required", label: "Sensor", kind: "select", required: true },
  {
    id: "named",
    label: "Anonymous",
    kind: "select",
    required: true,
    allLabel: "Anonymous excluded",
  },
]);

const options = [
  { value: "a", label: "Alpha" },
  { value: "b", label: "Beta" },
];

describe("FilterControl select", () => {
  it("offers an All row for an optional dimension", async () => {
    const onChange = vi.fn<(value: unknown) => void>();
    render(
      <FilterControl
        dim={optional!}
        value="a"
        options={options}
        onChange={onChange}
      />,
    );
    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.click(
      await screen.findByRole("option", { name: "All signals" }),
    );
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it("hides the All row for a required dimension", async () => {
    render(
      <FilterControl
        dim={required!}
        value="a"
        options={options}
        onChange={vi.fn<(value: unknown) => void>()}
      />,
    );
    await userEvent.click(screen.getByRole("combobox"));
    await screen.findByRole("option", { name: "Alpha" });
    expect(screen.queryByRole("option", { name: "All sensors" })).toBeNull();
  });

  it("keeps the All row when a required dimension names its empty state", async () => {
    const onChange = vi.fn<(value: unknown) => void>();
    render(
      <FilterControl
        dim={named!}
        value="included"
        options={[{ value: "included", label: "Anonymous included" }]}
        onChange={onChange}
      />,
    );
    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.click(
      await screen.findByRole("option", { name: "Anonymous excluded" }),
    );
    expect(onChange).toHaveBeenCalledWith(null);
  });
});
