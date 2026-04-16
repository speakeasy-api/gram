import { cn } from "@/lib/utils";

/**
 * Table wrapper that matches the DotCard aesthetic — rounded border,
 * subtle background, and consistent spacing with the card grid.
 */
export function DotTable({
  headers,
  children,
  className,
}: {
  headers: { label: string; className?: string }[];
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "!border-foreground/10 w-full overflow-hidden rounded-xl border",
        className,
      )}
    >
      <table className="w-full text-sm">
        <thead>
          <tr className="bg-muted/30 border-b">
            {/* Empty header for the dot-pattern column */}
            <th className="w-17" />
            {headers.map((header, index) => (
              <th
                key={header.label || `header-${index}`}
                className={cn(
                  "text-muted-foreground px-3 py-2.5 text-left text-xs font-medium tracking-wider uppercase",
                  header.className,
                )}
              >
                {header.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="[&_tr:last-child]:border-0">{children}</tbody>
      </table>
    </div>
  );
}
