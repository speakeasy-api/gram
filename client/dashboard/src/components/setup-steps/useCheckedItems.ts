import { useState } from "react";

/**
 * Which checklist items the operator has ticked, by block key. Kept for as
 * long as the setup stays mounted; ticks are a reading aid, never saved.
 */
export function useCheckedItems(): {
  isChecked: (blockKey: string) => boolean;
  setChecked: (blockKey: string, checked: boolean) => void;
} {
  const [checkedItems, setCheckedItems] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  return {
    isChecked: (blockKey) => checkedItems.has(blockKey),
    setChecked: (blockKey, checked) =>
      setCheckedItems((current) => {
        const next = new Set(current);
        if (checked) next.add(blockKey);
        else next.delete(blockKey);
        return next;
      }),
  };
}
