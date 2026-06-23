import { createContext } from "react";
import type { RiskResult } from "@gram/client/models/components";

// Provides the "Create exclusion" action to findings deep in the transcript.
// Null when the viewer lacks org:admin, which hides the action.
export const CreateExclusionContext = createContext<
  ((result: RiskResult) => void) | null
>(null);
