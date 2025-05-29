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

export function SkeletonCode({ lines = 24 }: { lines?: number }) {
  const importLines = Math.floor(lines / 4);
  const codeLines = lines - importLines;

  const LineNumber = () => <Skeleton className="h-5 w-6 mr-4 flex-shrink-0" />;

  const EmptyLine = () => <LineNumber />;

  const CodeLine = ({ width }: { width: string }) => (
    <div className="flex">
      <LineNumber />
      <Skeleton className={`h-5 ${width}`} />
    </div>
  );

  return (
    <div className="border rounded-lg p-4">
      <Stack gap={2}>
        {/* Import lines - typically shorter */}
        {Array.from({ length: importLines }).map((_, i) => (
          <CodeLine key={`import-${i}`} width="w-36" />
        ))}

        <EmptyLine key="spacer" />

        {/* Code lines with varying widths */}
        {Array.from({ length: codeLines }).map((_, i) => {
          // Create pattern of different line lengths
          if (i % 9 === 0) {
            return <EmptyLine key={`empty-${i}`} />;
          } else if (i % 9 === 1) {
            // Function declaration - medium line
            return <CodeLine key={`code-${i}`} width="w-1/2" />;
          } else if (i % 9 === 2) {
            // Opening bracket - very short
            return <CodeLine key={`code-${i}`} width="w-8" />;
          } else if (i % 9 === 7) {
            // Closing bracket - very short
            return <CodeLine key={`code-${i}`} width="w-8" />;
          } else if (i % 9 === 8) {
            // Empty line after function
            return <EmptyLine key={`empty-${i}`} />;
          } else if (i % 9 === 3 || i % 9 === 5) {
            // Longer indented lines
            return <CodeLine key={`code-${i}`} width="w-3/4" />;
          } else {
            // Regular indented lines - medium length
            return <CodeLine key={`code-${i}`} width="w-1/2" />;
          }
        })}
      </Stack>
    </div>
  );
}

export { Skeleton };
