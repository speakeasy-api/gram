import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineFilters } from "./filter-schema";
import { FilterControl } from "./FilterControl";

const multi = vi.hoisted(() => ({
  props: undefined as
    | {
        value?: string[];
        onValueChange: (values: string[]) => void;
        "aria-describedby"?: string;
      }
    | undefined,
}));

vi.mock("@/components/ui/MultiSelect", () => ({
  MultiSelect: (props: typeof multi.props) => {
    multi.props = props;
    return null;
  },
}));

afterEach(cleanup);

const [capped, open] = defineFilters([
  { id: "person", label: "Person", kind: "multiselect", maxSelected: 2 },
  { id: "step", label: "Step", kind: "multiselect" },
]);

describe("FilterControl with maxSelected", () => {
  it("refuses a pick past the cap, lets values go, and says so", () => {
    const onChange = vi.fn<(value: unknown) => void>();
    const onRefused = vi.fn<() => void>();
    render(
      <FilterControl
        dim={capped}
        value={["a", "b"]}
        onChange={onChange}
        onRefused={onRefused}
        describedBy="hint"
      />,
    );
    expect(multi.props?.value).toEqual(["a", "b"]);
    expect(multi.props?.["aria-describedby"]).toBe("hint");
    multi.props?.onValueChange(["a", "b", "c"]);
    expect(onChange).not.toHaveBeenCalled();
    expect(onRefused).toHaveBeenCalledOnce();
    multi.props?.onValueChange(["a"]);
    expect(onChange).toHaveBeenCalledWith(["a"]);
  });

  it("leaves a dimension with no cap uncontrolled and unlimited", () => {
    const onChange = vi.fn<(value: unknown) => void>();
    render(<FilterControl dim={open} value={["a", "b"]} onChange={onChange} />);
    expect(multi.props?.value).toBeUndefined();
    multi.props?.onValueChange(["a", "b", "c"]);
    expect(onChange).toHaveBeenCalledWith(["a", "b", "c"]);
  });
});
