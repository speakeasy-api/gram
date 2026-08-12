import { useCallback, useEffect, useMemo, useState } from "react";

export type RowSelection<T> = {
  selectedIds: Set<string>;
  selectedCount: number;
  isSelected: (id: string) => boolean;
  toggle: (id: string) => void;
  toggleAll: () => void;
  clear: () => void;
  /** "checked" | "indeterminate" | false, for the header checkbox. */
  allState: boolean | "indeterminate";
  selectedItems: T[];
};

/** Bulk row-selection state for a list/table page. No such hook existed anywhere in
 * the dashboard prior to AIS-321 (bulk mark-false-positive) — every prior list page
 * only ever needed single-row actions. */
export function useRowSelection<T>(
  items: T[],
  getId: (item: T) => string,
): RowSelection<T> {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  const isSelected = useCallback(
    (id: string) => selectedIds.has(id),
    [selectedIds],
  );

  const toggle = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleAll = useCallback(() => {
    setSelectedIds((prev) => {
      const allSelected =
        items.length > 0 && items.every((i) => prev.has(getId(i)));
      if (allSelected) return new Set();
      return new Set(items.map(getId));
    });
  }, [items, getId]);

  const clear = useCallback(() => setSelectedIds(new Set()), []);

  // Prune ids that fell out of `items` (e.g. a filter change, or the item was
  // dismissed) so selectedCount/selectedItems can't stay stuck referencing
  // rows the user can no longer see or act on.
  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) return prev;
      const currentIds = new Set(items.map(getId));
      const next = new Set([...prev].filter((id) => currentIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [items, getId]);

  const allState = useMemo((): boolean | "indeterminate" => {
    if (items.length === 0 || selectedIds.size === 0) return false;
    const allSelected = items.every((i) => selectedIds.has(getId(i)));
    if (allSelected) return true;
    return "indeterminate";
  }, [items, getId, selectedIds]);

  const selectedItems = useMemo(
    () => items.filter((i) => selectedIds.has(getId(i))),
    [items, getId, selectedIds],
  );

  return {
    selectedIds,
    // Derived from selectedItems (current-list-filtered), not selectedIds.size,
    // so the count showing in the bulk-action bar always matches what the
    // action would actually operate on — pruning above is async (an effect),
    // so this avoids a one-frame mismatch after items changes.
    selectedCount: selectedItems.length,
    isSelected,
    toggle,
    toggleAll,
    clear,
    allState,
    selectedItems,
  };
}
