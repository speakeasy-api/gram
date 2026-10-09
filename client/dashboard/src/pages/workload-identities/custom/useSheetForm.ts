import { type Dispatch, type SetStateAction, useState } from "react";

/**
 * A sheet form's values and the row they edit, started over from what is
 * stored now each time the sheet opens.
 *
 * The sheet stays mounted between uses, so each opening resets the form. The
 * reset runs during the render that opens the sheet, because that render is
 * the one that hands over the row's values, and the form must start from
 * them for a save to write nothing stale.
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
