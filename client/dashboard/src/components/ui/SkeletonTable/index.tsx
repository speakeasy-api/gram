import { Table, type Column } from "../Table";
import { Skeleton, type SkeletonProps } from "../Skeleton";

/**
 * A five-row placeholder shaped like a data table. Pass `columns` (one grid
 * width per column) to match the table it stands in for.
 */
export function SkeletonTable({
  className,
  label,
  columns: widths = ["0.25fr", "0.5fr", "1fr"],
}: {
  className?: string;
  columns?: Column<{ a: string }>["width"][];
} & Pick<SkeletonProps, "label"> = {}): React.JSX.Element {
  const columns: Column<{ a: string }>[] = widths.map((width, i) => ({
    header: "",
    key: String(i),
    render: () => (
      <Skeleton>
        <div className="h-6 w-full" />
      </Skeleton>
    ),
    width,
  }));

  return (
    <div {...(label ? { role: "status", "aria-label": label } : {})}>
      <Table
        columns={columns}
        data={[{ a: "a" }, { a: "b" }, { a: "c" }, { a: "d" }, { a: "e" }]}
        rowKey={(row) => row.a}
        hideHeader
        className={className}
      />
    </div>
  );
}
