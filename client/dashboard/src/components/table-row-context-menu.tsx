import { ContextMenu, ContextMenuTrigger } from "./ui/context-menu";
import { ActionContextMenuContent } from "./card-context-menu";
import type { Action } from "./ui/more-actions";

/**
 * Row variant of CardContextMenu. Wraps a single row element (`<tr>`, list
 * row, row button) in a right-click menu of the same `Action[]` the row
 * feeds its visible "⋯" menu — keeping the two in sync. Uses `asChild`, so
 * `children` must be one element that forwards refs and props (DotRow,
 * moonshine Table rows via `renderRow`, native elements). Renders children
 * unwrapped when `actions` is empty, so it's a safe no-op to apply broadly.
 */
export function TableRowContextMenu({
  actions,
  children,
}: {
  actions: Action[];
  children: React.ReactElement;
}): React.JSX.Element {
  if (actions.length === 0) {
    return <>{children}</>;
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ActionContextMenuContent actions={actions} />
    </ContextMenu>
  );
}
