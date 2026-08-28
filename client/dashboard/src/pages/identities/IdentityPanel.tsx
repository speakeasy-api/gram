import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ArrowUpRight } from "lucide-react";
import * as React from "react";
import { Link } from "react-router";

/**
 * A panel on an identity page. Every panel shows one subsystem's slice of the
 * person and names the page that owns that data, so the identity page reads as
 * a junction rather than as a second copy of the tool it links to.
 */
export function IdentityPanel({
  title,
  handoffLabel,
  handoffHref,
  footer,
  children,
  className,
}: {
  title: string;
  /** The page this panel continues on, e.g. "Audit Logs". */
  handoffLabel?: string;
  handoffHref?: string;
  /** One line saying what the slice is, e.g. "7 of 412 project findings". */
  footer?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  return (
    <section
      className={cn("bg-card border-border flex flex-col border", className)}
    >
      <header className="border-border flex items-center justify-between gap-3 border-b px-4 py-3">
        <h3 className="text-sm font-medium">{title}</h3>
        {handoffLabel && handoffHref && (
          <Link
            to={handoffHref}
            className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 text-xs"
          >
            Open in {handoffLabel}
            <ArrowUpRight className="size-3" />
          </Link>
        )}
      </header>
      <div className="flex-1">{children}</div>
      {footer && (
        <footer className="border-border border-t px-4 py-2.5">
          <Text variant="small" muted className="text-xs">
            {footer}
          </Text>
        </footer>
      )}
    </section>
  );
}

/** A row inside a panel: what happened, its detail, and a trailing value. */
export function IdentityPanelRow({
  title,
  detail,
  trailing,
  accent,
}: {
  title: React.ReactNode;
  detail?: React.ReactNode;
  trailing?: React.ReactNode;
  /** Draws the leading marker that flags a row as needing attention. */
  accent?: "destructive" | "warning";
}): React.JSX.Element {
  return (
    <div className="border-border flex items-center gap-3 border-b px-4 py-3 last:border-b-0">
      {accent && (
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            accent === "destructive" ? "bg-destructive" : "bg-warning",
          )}
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">{title}</div>
        {detail && (
          <div className="text-muted-foreground truncate text-xs">{detail}</div>
        )}
      </div>
      {trailing && (
        <div className="text-muted-foreground shrink-0 font-mono text-xs">
          {trailing}
        </div>
      )}
    </div>
  );
}

export function IdentityPanelEmpty({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div className="px-4 py-6">
      <Text variant="small" muted className="text-xs">
        {children}
      </Text>
    </div>
  );
}
