import { cn } from "@/lib/utils";
import { Column, Table } from "@speakeasy-api/moonshine";

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("bg-accent animate-pulse rounded-md", className)}
      {...props}
    />
  );
}

export function SkeletonTable() {
  const columns: Column<{ a: string }>[] = [
    {
      header: "Name",
      key: "1",
      render: () => <Skeleton className="h-6 w-full" />,
      width: "0.25fr",
    },
    {
      header: "Name",
      key: "2",
      render: () => <Skeleton className="h-6 w-full" />,
      width: "0.5fr",
    },
    {
      header: "Name",
      key: "3",
      render: () => <Skeleton className="h-6 w-full" />,
    },
  ];

  return (
    <Table
      columns={columns}
      data={[{ a: "a" }, { a: "b" }, { a: "c" }, { a: "d" }, { a: "e" }]}
      rowKey={(row) => row.a}
      hideHeader
    />
  );
}

export { Skeleton };
