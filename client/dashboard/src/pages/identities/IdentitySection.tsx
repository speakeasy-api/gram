import * as React from "react";

/**
 * A sub-page's own header: the section name, and one line saying what the
 * slice is. Lighter than the page title — the person named in the page header
 * above is the subject, and the rail already says which section is open.
 */
export function IdentitySection({
  title,
  meta,
  action,
  children,
}: {
  title?: string;
  /** e.g. "7 findings · 3 challenges · 11 blocks". */
  meta?: React.ReactNode;
  /** A control scoping everything below it, e.g. the account-type filter. */
  action?: React.ReactNode;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <section className="flex flex-col gap-6">
      {(title || meta || action) && (
        <div className="border-border flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b pb-3">
          {title && <h2 className="text-display-xs font-thin">{title}</h2>}
          <div className="flex items-center gap-4">
            {meta && (
              <span className="text-muted-foreground font-mono text-xs">
                {meta}
              </span>
            )}
            {action}
          </div>
        </div>
      )}
      {children}
    </section>
  );
}
