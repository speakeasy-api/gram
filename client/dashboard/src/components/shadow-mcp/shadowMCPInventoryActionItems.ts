import type { Action } from "@/components/ui/more-actions";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { InventoryActionMode } from "./ShadowMCPInventoryActions";

export const ALLOW_RULE_POLICY_REQUIRED =
  "An enabled blocking Shadow MCP policy is required.";

/**
 * The per-server action set, shared by the visible "⋯" dropdown and the row's
 * right-click context menu so the two stay in sync. The entries are additive
 * over the server's state — a server with both a pending request and an
 * existing allow rule offers Review Request AND Edit/Delete Rule. Each
 * onClick defers via setTimeout so the menu can close before a sheet opens.
 */
export function shadowMCPInventoryActions(
  server: ShadowMCPInventoryServer,
  {
    canManageAllowRules,
    disabled,
    onOpenAction,
  }: {
    canManageAllowRules: boolean;
    disabled: boolean;
    onOpenAction: (
      mode: InventoryActionMode,
      server: ShadowMCPInventoryServer,
    ) => void;
  },
): Action[] {
  const hasRequest = server.requestCount > 0;
  const hasAllowDecision = server.access === "allowed";
  const allowRuleDisabled = disabled || !canManageAllowRules;
  const allowRuleDescription = canManageAllowRules
    ? undefined
    : ALLOW_RULE_POLICY_REQUIRED;

  const openAction = (mode: InventoryActionMode) => () => {
    window.setTimeout(() => onOpenAction(mode, server), 0);
  };

  const actions: Action[] = [];
  if (hasRequest) {
    actions.push({
      label: "Review Request",
      disabled,
      onClick: openAction("review"),
    });
  }
  if (!hasRequest && !hasAllowDecision) {
    actions.push({
      label: "Add Allow Rule",
      description: allowRuleDescription,
      disabled: allowRuleDisabled,
      onClick: openAction("add"),
    });
  }
  if (hasAllowDecision) {
    actions.push(
      {
        label: "Edit Rule",
        description: allowRuleDescription,
        disabled: allowRuleDisabled,
        onClick: openAction("edit"),
      },
      {
        label: "Delete Rule",
        destructive: true,
        disabled,
        onClick: openAction("delete"),
      },
    );
  }
  return actions;
}
