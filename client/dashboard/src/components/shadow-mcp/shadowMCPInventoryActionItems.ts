import type { Action } from "@/components/ui/more-actions";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import type { InventoryActionMode } from "./ShadowMCPInventoryActions";

/**
 * The per-server action set, shared by the visible "⋯" dropdown and the row's
 * right-click context menu so the two stay in sync. Which entries appear is a
 * state machine over the server's request/allow-decision state. Each onClick
 * defers via setTimeout so the menu can close before a sheet opens.
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

  if (hasRequest) {
    return [
      { label: "Review Request", disabled, onClick: openAction("review") },
    ];
  }
  if (!hasAllowDecision) {
    return [{ label: "Add Allow Rule", disabled, onClick: openAction("add") }];
  }
  return [
    { label: "Edit Rule", disabled, onClick: openAction("edit") },
    {
      label: "Delete Rule",
      destructive: true,
      disabled,
      onClick: openAction("delete"),
    },
  ];
}
