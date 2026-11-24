import { defineFilters } from "@/components/filters/filter-schema";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LogFilterSheet } from "./LogFilterSheet";

const multi = vi.hoisted(() => ({
  props: {} as Record<string, { onValueChange: (values: string[]) => void }>,
}));

vi.mock("@/components/ui/MultiSelect", () => ({
  MultiSelect: (props: {
    "aria-describedby"?: string;
    onValueChange: (values: string[]) => void;
  }) => {
    multi.props[props["aria-describedby"] ?? ""] = props;
    return null;
  },
}));

afterEach(cleanup);

const schema = defineFilters([
  { id: "person", label: "Person", kind: "multiselect", maxSelected: 2 },
  { id: "server", label: "Server", kind: "multiselect", maxSelected: 2 },
]);

const refusedHint = "You can pick up to 2. That one was not added.";

function renderSheet() {
  render(
    <LogFilterSheet
      open
      onOpenChange={vi.fn()}
      accents={{}}
      schema={schema}
      values={{ person: ["a", "b"], server: [] }}
      optionsById={{}}
      facets={undefined}
      total={undefined}
      onChange={vi.fn()}
      onReset={vi.fn()}
    />,
  );
}

function refusePerson() {
  act(() =>
    multi.props["log-filter-person-limit"]?.onValueChange(["a", "b", "c"]),
  );
  expect(screen.getByText(refusedHint)).toBeTruthy();
}

describe("LogFilterSheet refused pick", () => {
  it("clears the refusal when another filter changes", () => {
    renderSheet();
    refusePerson();
    act(() => multi.props["log-filter-server-limit"]?.onValueChange(["s"]));
    expect(screen.queryByText(refusedHint)).toBeNull();
  });

  it("clears the refusal on reset", () => {
    renderSheet();
    refusePerson();
    fireEvent.click(screen.getByRole("button", { name: "Reset filters" }));
    expect(screen.queryByText(refusedHint)).toBeNull();
  });
});
