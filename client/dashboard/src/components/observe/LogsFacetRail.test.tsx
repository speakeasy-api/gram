import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LogsFacetRail, type FacetGroup } from "./LogsFacetRail";

afterEach(cleanup);

const value = (name: string, selected = false, matchesSearch = true) => ({
  value: name,
  label: name,
  count: 1,
  selected,
  matchesSearch,
});

function renderRail(group: FacetGroup) {
  return render(
    <LogsFacetRail
      groups={[group]}
      onToggle={() => {}}
      onClearGroup={() => {}}
    />,
  );
}

it("filters a local group in place once it is long enough", () => {
  renderRail({
    id: "status",
    label: "Status",
    values: Array.from({ length: 11 }, (_, i) => value(`value ${i}`, i === 0)),
  });
  fireEvent.change(screen.getByLabelText("Filter Status"), {
    target: { value: "value 7" },
  });
  expect(screen.getByText("value 7")).toBeTruthy();
  expect(screen.getByText("value 0")).toBeTruthy();
  expect(screen.queryByText("value 3")).toBeNull();
});

it("searches the source for a group that holds more than it lists", () => {
  const onSearch = vi.fn<(text: string) => void>();
  const group: FacetGroup = {
    id: "client",
    label: "Client",
    totalCount: 40,
    onSearch,
    values: [value("Cursor", true), value("Zed")],
  };
  const { rerender } = renderRail(group);
  expect(screen.getByText("38 more — search to find them")).toBeTruthy();

  fireEvent.change(screen.getByLabelText("Filter Client"), {
    target: { value: "wind" },
  });
  expect(onSearch).toHaveBeenLastCalledWith("wind");
  expect(screen.getByText("Zed")).toBeTruthy();

  rerender(
    <LogsFacetRail
      groups={[
        {
          ...group,
          totalCount: 1,
          values: [value("Cursor", true, false), value("Windsurf")],
        },
      ]}
      onToggle={() => {}}
      onClearGroup={() => {}}
    />,
  );
  expect(screen.getByLabelText("Filter Client")).toBeTruthy();
  expect(screen.getByText("Windsurf")).toBeTruthy();
  expect(screen.getByText("Cursor")).toBeTruthy();
  expect(screen.queryByText(/more — search/)).toBeNull();

  fireEvent.click(screen.getByLabelText("Clear Client filter"));
  expect(onSearch).toHaveBeenLastCalledWith("");
});

it("counts only the returned values the source says match", () => {
  renderRail({
    id: "identity",
    label: "Identity",
    totalCount: 5,
    onSearch: () => {},
    values: [
      value("Selected match", true),
      value("Selected elsewhere", true, false),
      value("Match"),
    ],
  });
  expect(screen.getByText("3 more — search to find them")).toBeTruthy();
});

it("never claims more for a group it lists in full", () => {
  renderRail({
    id: "identity",
    label: "Identity",
    totalCount: 1,
    onSearch: () => {},
    values: [value("Match"), value("Selected elsewhere", true, false)],
  });
  expect(screen.queryByText(/more — search/)).toBeNull();
});
