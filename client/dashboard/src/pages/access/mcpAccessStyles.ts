/**
 * The dashboard's checkbox-list idiom (the role editor's member list, the
 * grant picker): one hairline box, divided rows, a muted hover, and a faint
 * primary wash on the rows that are chosen.
 */
export const LIST_FRAME = "border-border divide-border bg-card divide-y border";
export const LIST_ROW =
  "hover:bg-muted/50 flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2";
export const LIST_ROW_SELECTED = "bg-primary/5 hover:bg-primary/10";
