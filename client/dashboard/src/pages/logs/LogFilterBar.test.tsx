import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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

it("selects a clicked suggestion instead of committing the typed text", () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  render(
    <Bar
      onChange={onChange}
      keyOptions={[{ key: "identity" }]}
      operatorsFor={() => [Op.Eq]}
      valueOptions={[{ value: "agent:a-1", label: "Ops Bot" }]}
    />,
  );
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("identity"));
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "ops" },
  });
  const item = option("Ops Bot");
  expect(fireEvent.mouseDown(item)).toBe(false);
  fireEvent.click(item);
  expect(onChange).toHaveBeenCalledOnce();
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ path: "identity", value: "agent:a-1" }),
  ]);
});

it("dismisses value suggestions from outside and clears the value search", async () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  const onValueInputChange = vi.fn<(key: string, text: string) => void>();
  render(
    <div>
      <button type="button">outside</button>
      <Bar
        onChange={onChange}
        keyOptions={[{ key: "identity" }]}
        operatorsFor={() => [Op.Eq]}
        valueOptions={[{ value: "user:q", label: "Quiet Person" }]}
        onValueInputChange={onValueInputChange}
      />
    </div>,
  );
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("identity"));
  expect(screen.getByText("Quiet Person")).toBeTruthy();
  screen.getByRole("button", { name: "outside" }).focus();
  await waitFor(() => expect(screen.queryByText("Quiet Person")).toBeNull());
  expect(screen.queryByPlaceholderText("value")).toBeNull();
  expect(onChange).not.toHaveBeenCalled();
  expect(onValueInputChange).toHaveBeenLastCalledWith("identity", "");
});

it("commits a typed value once when dismissed from outside", async () => {
  const onChange = vi.fn<(filters: ActiveLogFilter[]) => void>();
  function Controlled() {
    const [filters, setFilters] = useState<ActiveLogFilter[]>([]);
    return (
      <div>
        <button type="button">outside</button>
        <Bar
          filters={filters}
          onChange={(next) => {
            onChange(next);
            setFilters(next);
          }}
          keyOptions={[{ key: "identity" }]}
          operatorsFor={() => [Op.Eq, Op.NotEq]}
          valueOptions={[{ value: "user:q", label: "Quiet Person" }]}
        />
      </div>
    );
  }
  render(<Controlled />);
  fireEvent.focus(screen.getByRole("combobox"));
  fireEvent.click(option("identity"));
  fireEvent.click(option("!="));
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "repo:x" },
  });
  screen.getByRole("button", { name: "outside" }).focus();
  await waitFor(() =>
    expect(screen.queryByPlaceholderText("value")).toBeNull(),
  );
  expect(onChange).toHaveBeenCalledOnce();
  expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({
      path: "identity",
      op: Op.NotEq,
      value: "repo:x",
    }),
  ]);
});

it("forgets an earlier refusal when the chip editor reopens", async () => {
  render(
    <Bar
      filters={[{ id: "c1", path: "client", op: Op.Eq, value: "c-1" }]}
      validateFilter={(_path, _op, value) =>
        value === "bad" ? "Not a client" : null
      }
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Edit filter client" }));
  const input = await screen.findByPlaceholderText("value");
  fireEvent.change(input, { target: { value: "bad" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.getByText("Not a client")).toBeTruthy();

  fireEvent.keyDown(input, { key: "Escape" });
  await waitFor(() => expect(screen.queryByText("Not a client")).toBeNull());
  fireEvent.click(screen.getByRole("button", { name: "Edit filter client" }));
  await screen.findByRole("button", { name: "Save" });
  expect(screen.queryByText("Not a client")).toBeNull();
});

it("leaves Enter on a chip's buttons to the browser and still submits from the input", () => {
  const onSearchSubmit = vi.fn<(query: string) => void>();
  render(
    <Bar
      filters={[{ id: "c1", path: "client", op: Op.Eq, value: "c-1" }]}
      initialInput="dana"
      onSearchSubmit={onSearchSubmit}
    />,
  );
  // fireEvent returns false when a handler cancelled the event, which would
  // stop the browser from clicking the focused button.
  for (const name of ["Edit filter client", "Remove filter client"]) {
    expect(
      fireEvent.keyDown(screen.getByRole("button", { name }), { key: "Enter" }),
    ).toBe(true);
  }
  expect(onSearchSubmit).not.toHaveBeenCalled();

  fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
  expect(onSearchSubmit).toHaveBeenCalledTimes(1);
  expect(onSearchSubmit).toHaveBeenLastCalledWith("dana");
});
