import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import { useState, type ComponentProps } from "react";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { LogFilterBar } from "./LogFilterBar";
import type { ActiveLogFilter } from "./log-filter-types";

beforeAll(() => {
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  if (!("scrollIntoView" in Element.prototype)) {
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      value: () => {},
    });
  }
});

afterEach(cleanup);

it("builds a chip from fixed fields, allowed operators and offered values", () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  const onValueInputChange = vi.fn<(key: string, text: string) => void>();
  render(
    <LogFilterBar
      filters={[{ id: "c1", path: "mcp_server", op: Op.Eq, value: "s1" }]}
      onChange={onChange}
      attributeKeys={[]}
      keyOptions={[
        {
          key: "identity",
          description: "Person, agent, workload or API key",
          count: 47,
        },
        { key: "last_used", description: "< 1h, < 24h, > 7d, never" },
      ]}
      operatorsFor={(key) =>
        key === "last_used" ? [Op.Eq] : [Op.Eq, Op.NotEq]
      }
      valueOptions={[{ value: "user:q", label: "Quiet Person", count: 2 }]}
      onValueInputChange={onValueInputChange}
      formatValue={(_, value) => (value === "s1" ? "Linear MCP" : value)}
      searchInput=""
      onSearchInputChange={() => {}}
      onSearchSubmit={() => {}}
    />,
  );

  expect(
    screen.getByRole("button", { name: "Edit filter mcp_server" }).textContent,
  ).toBe("mcp_server = Linear MCP");

  fireEvent.focus(screen.getByRole("combobox"));
  expect(screen.getByText("Person, agent, workload or API key")).toBeTruthy();
  expect(screen.getByText("47")).toBeTruthy();

  fireEvent.click(screen.getByText("identity"));
  expect(screen.getByText("!=")).toBeTruthy();
  expect(screen.queryByText("in")).toBeNull();
  fireEvent.click(screen.getByText("="));

  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "qu" },
  });
  expect(onValueInputChange).toHaveBeenLastCalledWith("identity", "qu");
  fireEvent.click(screen.getByText("Quiet Person"));
  expect(onChange).toHaveBeenLastCalledWith([
    { id: "c1", path: "mcp_server", op: Op.Eq, value: "s1" },
    expect.objectContaining({ path: "identity", op: Op.Eq, value: "user:q" }),
  ]);
});

type BarProps = ComponentProps<typeof LogFilterBar>;

function Bar(props: Partial<BarProps> & { initialInput?: string }) {
  const [searchInput, setSearchInput] = useState(props.initialInput ?? "");
  return (
    <LogFilterBar
      filters={[]}
      onChange={() => {}}
      attributeKeys={[]}
      onSearchSubmit={() => {}}
      {...props}
      searchInput={searchInput}
      onSearchInputChange={setSearchInput}
    />
  );
}

const option = (label: string) =>
  screen
    .getAllByRole("option")
    .find((o) =>
      [...o.querySelectorAll("span"), o].some((el) => el.textContent === label),
    )!;

it("keeps the default search flow when no applied search is given", () => {
  const onSearchSubmit = vi.fn<(query: string) => void>();
  render(<Bar initialInput="dana" onSearchSubmit={onSearchSubmit} />);
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
  expect(onSearchSubmit).toHaveBeenLastCalledWith("dana");
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "" } });
  expect(onSearchSubmit).toHaveBeenLastCalledWith("");
  expect(screen.queryByRole("button", { name: "Remove search" })).toBeNull();
});

it("shows an applied search beside a draft and removes it on its own", () => {
  const onSearchSubmit = vi.fn<(query: string) => void>();
  render(
    <Bar
      appliedSearch="dana"
      initialInput="draft"
      onSearchSubmit={onSearchSubmit}
    />,
  );
  expect(screen.getByText("search: dana")).toBeTruthy();
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "" } });
  expect(onSearchSubmit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Remove search" }));
  expect(onSearchSubmit).toHaveBeenCalledWith("");
});

it("skips the operator for a one-operator field and reads its chip plainly", () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  render(
    <Bar
      filters={[{ id: "l1", path: "last_used", op: Op.Eq, value: "> 7d" }]}
      onChange={onChange}
      keyOptions={[{ key: "last_used", label: "Last used" }]}
      operatorsFor={() => [Op.Eq]}
      valueNotice="Pick a window"
    />,
  );
  expect(
    screen.getByRole("button", { name: "Edit filter Last used" }).textContent,
  ).toBe("Last used > 7d");
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("Last used"));
  expect(screen.getByPlaceholderText("value")).toBeTruthy();
  expect(screen.queryByText("!=")).toBeNull();
  expect(screen.getByText("Pick a window")).toBeTruthy();
});

it("refuses a filter the caller says would not apply", () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  const onSearchSubmit = vi.fn<(query: string) => void>();
  render(
    <Bar
      initialInput="client = robot"
      onChange={onChange}
      onSearchSubmit={onSearchSubmit}
      validateFilter={(path) => (path === "client" ? "Not a client" : null)}
    />,
  );
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
  expect(screen.getByText("Not a client")).toBeTruthy();
  expect(onChange).not.toHaveBeenCalled();
  expect(onSearchSubmit).not.toHaveBeenCalled();
});

it("encodes an offered in-value and keeps exact values unless trimmed", () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  const { unmount } = render(
    <Bar
      onChange={onChange}
      keyOptions={[{ key: "identity" }]}
      operatorsFor={() => [Op.Eq, Op.In]}
      valueOptions={[{ value: "a,b%", label: "Awkward" }]}
      encodeInValue={encodeURIComponent}
      exactValue={(path) => path === "identity"}
    />,
  );
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("identity"));
  fireEvent.click(option("in"));
  fireEvent.click(option("Awkward"));
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ op: Op.In, value: "a%2Cb%25" }),
  ]);
  unmount();

  render(
    <Bar
      onChange={onChange}
      keyOptions={[{ key: "identity" }, { key: "client" }]}
      operatorsFor={() => [Op.Eq]}
      exactValue={(path) => path === "identity"}
    />,
  );
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("identity"));
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: " padded " },
  });
  fireEvent.keyDown(screen.getByPlaceholderText("value"), { key: "Enter" });
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ path: "identity", value: " padded " }),
  ]);
});
