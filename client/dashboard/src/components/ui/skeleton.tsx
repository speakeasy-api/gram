import { cn } from "@/lib/utils";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";

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

export function SkeletonParagraph({ lines = 3 }: { lines?: number }) {
  return (
    <Stack gap={2}>
      {Array.from({ length: lines - 1 }).map((_, i) => (
        <Skeleton key={i} className="h-4 w-full" />
      ))}
      <Skeleton className="h-4 w-[200px]" />
    </Stack>
  );
}

export { Skeleton };
