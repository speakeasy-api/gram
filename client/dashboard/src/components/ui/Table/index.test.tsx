import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { type Column, Table } from "@/components/ui/Table";

afterEach(cleanup);

type Row = {
  id: string;
  name: string;
  clickable?: boolean;
};

const columns: Column<Row>[] = [
  {
    key: "name",
    header: "Name",
    render: (row) => row.name,
  },
];

const data: Row[] = [
  { id: "1", name: "Alpha", clickable: true },
  { id: "2", name: "Beta", clickable: false },
];

function rowKey(row: Row): string {
  return row.id;
}

describe("Table row keyboard accessibility (DNO-758)", () => {
  it("makes clickable rows focusable and activates on Enter and Space", () => {
    const onRowClick = vi.fn<(row: Row) => void>();

    render(
      <Table
        columns={columns}
        data={data}
        rowKey={rowKey}
        onRowClick={onRowClick}
      />,
    );

    const alphaRow = screen.getByText("Alpha").closest("tr");
    expect(alphaRow).not.toBeNull();
    expect(alphaRow!.getAttribute("tabindex")).toBe("0");

    alphaRow!.focus();
    fireEvent.keyDown(alphaRow!, { key: "Enter" });
    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(onRowClick).toHaveBeenCalledWith(
      expect.objectContaining({ id: "1", name: "Alpha" }),
    );

    fireEvent.keyDown(alphaRow!, { key: " " });
    expect(onRowClick).toHaveBeenCalledTimes(2);
  });

  it("keeps non-clickable rows non-focusable when isRowClickable returns false", () => {
    const onRowClick = vi.fn<(row: Row) => void>();

    render(
      <Table columns={columns}>
        <Table.Header columns={columns} />
        <Table.Body
          columns={columns}
          data={data}
          rowKey={rowKey}
          onRowClick={onRowClick}
          isRowClickable={(row) => row.clickable !== false}
        />
      </Table>,
    );

    const alphaRow = screen.getByText("Alpha").closest("tr");
    const betaRow = screen.getByText("Beta").closest("tr");

    expect(alphaRow!.getAttribute("tabindex")).toBe("0");
    expect(betaRow!.getAttribute("tabindex")).toBeNull();

    fireEvent.keyDown(betaRow!, { key: "Enter" });
    fireEvent.click(betaRow!);
    expect(onRowClick).not.toHaveBeenCalled();

    fireEvent.keyDown(alphaRow!, { key: "Enter" });
    expect(onRowClick).toHaveBeenCalledTimes(1);
  });

  it("does not activate the row when Enter/Space originates from a nested control", () => {
    const onRowClick = vi.fn<(row: Row) => void>();
    const onButtonClick = vi.fn<() => void>();

    const columnsWithAction: Column<Row>[] = [
      {
        key: "name",
        header: "Name",
        render: (row) => (
          <span>
            {row.name}
            <button type="button" onClick={onButtonClick}>
              Edit {row.name}
            </button>
          </span>
        ),
      },
    ];

    render(
      <Table
        columns={columnsWithAction}
        data={[{ id: "1", name: "Alpha" }]}
        rowKey={rowKey}
        onRowClick={onRowClick}
      />,
    );

    const row = screen.getByText("Alpha").closest("tr");
    const button = screen.getByRole("button", { name: "Edit Alpha" });

    button.focus();
    fireEvent.keyDown(button, { key: "Enter" });
    fireEvent.keyDown(button, { key: " " });

    expect(onRowClick).not.toHaveBeenCalled();
    expect(row!.getAttribute("tabindex")).toBe("0");
  });

  it("does not make rows focusable when onRowClick is omitted", () => {
    render(<Table columns={columns} data={data} rowKey={rowKey} />);

    const alphaRow = screen.getByText("Alpha").closest("tr");
    expect(alphaRow!.getAttribute("tabindex")).toBeNull();
  });
});
