/**
 * The empty tool log of a view scoped to one identity: that identity made no
 * calls in the period, not that capture is missing for the project. When the
 * view hides local tools it says so, since those calls may still exist.
 */
export function ScopedToolLogsEmptyState({
  localToolsHidden,
}: {
  localToolsHidden: boolean;
}): JSX.Element {
  return (
    <div className="py-12 text-center">
      <span className="text-muted-foreground text-sm">
        No tool calls from this identity in this period.
        {localToolsHidden && " Local tools are hidden."}
      </span>
    </div>
  );
}
