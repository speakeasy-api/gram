import { useCommandPalette } from "@/contexts/CommandPalette";
import { useEffect } from "react";
import { useNavigate } from "react-router";

const GATEWAY_DEBUG_ACTION_ID = "gateway-debug";

/**
 * Offers the current gateway's Debug page in Command K while a gateway page is
 * open. A null href offers nothing, for a viewer who cannot open it.
 */
export function useGatewayDebugAction(href: string | null): void {
  const { addActions, removeActions } = useCommandPalette();
  const navigate = useNavigate();

  useEffect(() => {
    if (!href) return;
    addActions([
      {
        id: GATEWAY_DEBUG_ACTION_ID,
        label: "Debug this gateway",
        icon: "bug",
        group: "Gateway",
        onSelect: () => void navigate(href),
      },
    ]);
    return () => removeActions([GATEWAY_DEBUG_ACTION_ID]);
  }, [href, addActions, removeActions, navigate]);
}
