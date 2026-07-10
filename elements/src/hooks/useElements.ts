import { useContext } from "react";
import { ElementsContext } from "#elements/contexts/contexts";
import type { ElementsContextType } from "#elements/types";

/**
 * @private Internal hook to access the ElementsContext
 *
 */
export const useElements = (): ElementsContextType => {
  const context = useContext(ElementsContext);
  if (!context) {
    throw new Error("useElements must be used within a ElementsProvider");
  }
  return context;
};
