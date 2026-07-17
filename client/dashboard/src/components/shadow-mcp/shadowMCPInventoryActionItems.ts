import type { Action } from "@/components/ui/more-actions";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { InventoryActionMode } from "./ShadowMCPInventoryActions";

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
    disabled,
    onOpenAction,
  }: {
    disabled: boolean;
    onOpenAction: (
      mode: InventoryActionMode,
      server: ShadowMCPInventoryServer,
    ) => void;
  },
): Action[] {
  const hasRequest = server.requestCount > 0;
  const hasAllowDecision = server.access === "allowed";

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
      disabled,
      onClick: openAction("add"),
    });
  }
  if (hasAllowDecision) {
    actions.push(
      { label: "Edit Rule", disabled, onClick: openAction("edit") },
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
