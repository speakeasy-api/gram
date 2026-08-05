import { cn } from "@/lib/utils";

/**
 * Amber "DEV" pill marking UI that only Speakeasy platform admins (or local
 * dev builds) can see. Shared by the floating developer toolbar and any
 * admin-only form fields.
 */
export function DevBadge({ className }: { className?: string }): JSX.Element {
  return (
    <span
      className={cn(
        "rounded bg-amber-100 px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-widest text-amber-700 dark:bg-amber-900/40 dark:text-amber-400",
        className,
      )}
    >
      DEV
    </span>
  );
}
