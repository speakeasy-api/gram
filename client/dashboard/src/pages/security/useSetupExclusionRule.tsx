import { useCallback, useRef, useState, type JSX } from "react";
import { toast } from "sonner";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskSuggestExclusionMutation } from "@gram/client/react-query/riskSuggestExclusion.js";
import { findingToExclusionState } from "@/pages/chatLogs/chatHelpers";
import { BUILTIN_RULE_ID_LIST } from "./detection-rules-data";
import { serializeExclusionExpression } from "./exclusion-expression";
import {
  ExclusionSheet,
  GLOBAL_SCOPE,
  type ExclusionSheetState,
} from "./exclusion-sheet";

// A batch AI-suggestion attempt gets this long to resolve before the sheet
// opens unprefilled — long enough for a real completion, short enough that
// selecting a batch and asking for an exclusion never feels stuck.
const SUGGEST_TIMEOUT_MS = 6000;

/** Opens the "Setup exclusion rule" sheet from a selection of one or more
 * findings — shared by every surface that offers both "Mark as false
 * positive" and exclusion setup side by side (Risk Events, Risk Overview
 * Category Detail; the chat transcript wires its own single-finding case
 * directly via CreateExclusionContext instead, since it predates this hook).
 *
 * A single finding prefills the expression directly, mirroring the chat
 * transcript's per-finding flow (same `findingToExclusionState` helper). Two
 * or more triggers an AI-suggested pattern derived from the batch — the
 * caller should show `isSuggesting` as a spinner on its trigger button. The
 * sheet still opens (unprefilled, so the operator can finish manually)
 * rather than dead-ending on a timeout or a failed/empty suggestion. */
export function useSetupExclusionRule(): {
  open: (results: RiskResult[]) => void;
  isSuggesting: boolean;
  sheet: JSX.Element;
} {
  const [sheetState, setSheetState] = useState<ExclusionSheetState | null>(
    null,
  );
  const [isSuggesting, setIsSuggesting] = useState(false);
  const suggestMutation = useRiskSuggestExclusionMutation();
  // Bumped on every open() call. Promise.race doesn't cancel the losing
  // request — after a timeout falls back to the manual sheet, the actual
  // mutation keeps running in the background and eventually still resolves.
  // Without this token, that late resolution (or a second open() call for a
  // different selection made in the meantime) would clobber whatever sheet
  // state the operator is looking at by then and leave the spinner on past
  // the fallback.
  const requestIdRef = useRef(0);

  const open = useCallback(
    (results: RiskResult[]) => {
      if (results.length === 0) return;

      const requestId = ++requestIdRef.current;

      if (results.length === 1) {
        setIsSuggesting(false);
        setSheetState(findingToExclusionState(results[0]!));
        return;
      }

      setIsSuggesting(true);

      const timedOut = new Promise<"timeout">((resolve) => {
        setTimeout(() => resolve("timeout"), SUGGEST_TIMEOUT_MS);
      });
      void Promise.race([
        suggestMutation.mutateAsync({
          request: {
            suggestExclusionRequestBody: {
              findingIds: results.map((r) => r.id),
              knownRuleIds: BUILTIN_RULE_ID_LIST,
            },
          },
        }),
        timedOut,
      ])
        .then((result) => {
          if (requestId !== requestIdRef.current) return;
          setIsSuggesting(false);
          if (result === "timeout") {
            toast.error("AI suggestion took too long. Setting up manually.");
            setSheetState({ mode: "create", initialScope: GLOBAL_SCOPE });
            return;
          }
          if (!result.matchType || !result.matchValue) {
            toast.error("No suggestion came back. Setting up manually.");
            setSheetState({ mode: "create", initialScope: GLOBAL_SCOPE });
            return;
          }
          setSheetState({
            mode: "create",
            initialExpression: serializeExclusionExpression({
              matchType: result.matchType,
              matchValue: result.matchValue,
              ruleIdFilter: result.ruleIdFilter ?? "",
              sourceFilter: result.sourceFilter ?? "",
            }),
            initialScope: GLOBAL_SCOPE,
          });
        })
        .catch(() => {
          if (requestId !== requestIdRef.current) return;
          setIsSuggesting(false);
          toast.error("AI suggestion failed. Setting up manually.");
          setSheetState({ mode: "create", initialScope: GLOBAL_SCOPE });
        });
    },
    [suggestMutation],
  );

  return {
    open,
    isSuggesting,
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
