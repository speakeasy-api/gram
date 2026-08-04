import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

/**
 * Page scaffolding shared by the XAA management pages: a title, a sentence
 * explaining what the rows actually decide, and a table.
 */
export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-sm text-muted-foreground max-w-2xl">{description}</p>
      </header>
      {children}
    </section>
  );
}

export function Table({
  headers,
  children,
  empty,
  isEmpty = false,
}: {
  headers: string[];
  children: ReactNode;
  empty?: string;
  /**
   * Whether to show the empty-state row instead of `children`. Passed
   * explicitly rather than inferred from `children`, because a caller mapping
   * an empty array still produces a truthy (empty-array) child.
   */
  isEmpty?: boolean;
}) {
  return (
    <div className="rounded-lg border border-border overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border">
            {headers.map((h) => (
              <th
                key={h}
                className="text-left font-medium text-[11px] uppercase tracking-wider text-muted-foreground/80 px-3 py-2"
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {isEmpty ? (
            <tr>
              <td
                colSpan={headers.length}
                className="px-3 py-6 text-center text-muted-foreground"
              >
                {empty ?? "Nothing here yet."}
              </td>
            </tr>
          ) : (
            children
          )}
        </tbody>
      </table>
    </div>
  );
}

export function Row({ children }: { children: ReactNode }) {
  return (
    <tr className="border-b border-border/60 last:border-0 align-middle">
      {children}
    </tr>
  );
}

export function Cell({
  children,
  mono = false,
  className,
}: {
  children: ReactNode;
  mono?: boolean;
  className?: string;
}) {
  return (
    <td
      className={cn(
        "px-3 py-2",
        mono && "font-mono text-xs break-all",
        className,
      )}
    >
      {children}
    </td>
  );
}

export function DeleteButton({
  onClick,
  disabled,
}: {
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <Button
      variant="destructive"
      size="sm"
      onClick={onClick}
      disabled={disabled}
    >
      Delete
    </Button>
  );
}

/** A dash for an empty value, so blank cells read as intentional. */
export function OrDash({ value }: { value: string }) {
  return value ? <>{value}</> : <span className="opacity-40">—</span>;
}
