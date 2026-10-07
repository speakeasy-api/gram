import { type Dispatch, type SetStateAction, useState } from "react";

/**
 * A sheet form's values and the row they edit, started over from what is
 * stored now each time the sheet opens.
 *
 * The sheet stays mounted between uses, so each opening resets the form. It
 * is done as the sheet opens, during render, because the page hands over a
 * row's values in the same render that opens the sheet: a reset on close
 * would keep what the form held before them, and the next save would write
 * those stale values back.
 *
 * The baseline is the row as it stood when the sheet opened. A query refresh
 * can replace `initial` mid-edit, and diffing against that would turn fields
 * the operator never touched into changes that overwrite the newer values.
 */
export function useSheetForm<I, V>(
  open: boolean,
  initial: I | undefined,
  toValues: (initial: I | undefined) => V,
): {
  values: V;
  setValues: Dispatch<SetStateAction<V>>;
  baseline: I | undefined;
} {
  const [values, setValues] = useState<V>(() => toValues(initial));
  const [baseline, setBaseline] = useState(initial);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setValues(toValues(initial));
      setBaseline(initial);
    }
  }
  return { values, setValues, baseline };
}
