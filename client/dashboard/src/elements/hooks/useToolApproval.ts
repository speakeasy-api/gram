import { useContext } from "react";
import { ToolApprovalContext } from "@/elements/contexts/contexts";
import type { ToolApprovalContextType } from "@/elements/contexts/ToolApprovalContext";

/**
 * Hook to access the tool approval context for managing human-in-the-loop
 * tool execution approval.
 */
export const useToolApproval = (): ToolApprovalContextType => {
  const context = useContext(ToolApprovalContext);
  if (!context) {
    throw new Error(
      "useToolApproval must be used within a ToolApprovalProvider",
    );
  }
  return context;
};
