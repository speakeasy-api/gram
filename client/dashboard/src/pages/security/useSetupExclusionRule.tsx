import { useCallback, useState, type JSX } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { ExclusionSheet, type ExclusionSheetState } from "./exclusion-sheet";

/** Opens the "Setup exclusion rule" sheet from a selection of one or more
 * findings — shared by every surface that offers both "Mark as false
 * positive" and exclusion setup side by side (Risk Events, Risk Overview
 * Category Detail; the chat transcript wires its own single-finding case
 * directly via CreateExclusionContext instead, since it predates this hook).
 *
 * The sheet opens immediately on ready-made rules computed from the selection
 * itself (see `exclusionOptions`), single or batch alike. Nothing is fetched
 * here; the AI suggestion lives inside the sheet's "Write it myself" branch,
 * where the operator asks for it. */
export function useSetupExclusionRule(): {
  open: (results: RiskResult[]) => void;
  sheet: JSX.Element;
} {
  const [sheetState, setSheetState] = useState<ExclusionSheetState | null>(
    null,
  );

  const open = useCallback((results: RiskResult[]) => {
    if (results.length === 0) return;
    setSheetState({ mode: "create", results });
  }, []);

  return {
    open,
    sheet: (
      <ExclusionSheet
        state={sheetState}
        onOpenChange={(open) => {
          if (!open) setSheetState(null);
        }}
      />
    ),
  };
}
