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
        "w-full rounded-xl border !border-foreground/10 overflow-hidden",
        className,
      )}
    >
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/30">
            {/* Empty header for the dot-pattern column */}
            <th className="w-17" />
            {headers.map((header, index) => (
              <th
                key={header.label || `header-${index}`}
                className={cn(
                  "px-3 py-2.5 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider",
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
