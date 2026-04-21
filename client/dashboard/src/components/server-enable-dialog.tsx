import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useProductTier } from "@/hooks/useProductTier";
import { useOrgRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { CreditCard, Server } from "lucide-react";

interface ServerEnableDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  currentlyEnabled?: boolean;
  targetIsPublic?: boolean;
}

export function ServerEnableDialog({
  isOpen,
  onClose,
  onConfirm,
  isLoading = false,
  currentlyEnabled = false,
  targetIsPublic = false,
}: ServerEnableDialogProps) {
  const productTier = useProductTier();
  const orgRoutes = useOrgRoutes();
  const { data: periodUsage } = useGetPeriodUsage();

  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  const handleUpgrade = () => {
    orgRoutes.billing.goTo();
    onClose();
  };

  const hasAdditionalIncludedServers =
    periodUsage &&
    periodUsage.includedServers > periodUsage.actualEnabledServerCount;

  const canEnable =
    productTier === "base" && !currentlyEnabled
      ? hasAdditionalIncludedServers
      : true;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title className="flex items-center gap-2">
            <Server className="h-5 w-5" />
            {currentlyEnabled
              ? "Disable MCP Server"
              : targetIsPublic
                ? "Enable & Make Public"
                : "Enable MCP Server"}
          </Dialog.Title>
        </Dialog.Header>

        <div className="space-y-4">
          {!canEnable ? (
            <Type className="text-muted-foreground">
              Free accounts are limited to one enabled MCP server. To enable
              additional servers, upgrade to a paid plan.
            </Type>
          ) : (
            <Type className="text-muted-foreground">
              {currentlyEnabled
                ? "Disabling this server will stop all requests and may affect any applications using this MCP server."
                : targetIsPublic
                  ? "This will enable the server and make it publicly accessible. Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools."
                  : "Enabling this server will allow it to receive requests. Standard usage charges may apply based on your plan."}
            </Type>
          )}
        </div>

        <Dialog.Footer className="gap-2">
          <Button variant="tertiary" onClick={onClose}>
            Cancel
          </Button>
          {!canEnable ? (
            <Button onClick={handleUpgrade} className="gap-2">
              <CreditCard className="h-4 w-4" />
              Upgrade Plan
            </Button>
          ) : (
            <Button
              onClick={handleConfirm}
              disabled={isLoading}
              variant={currentlyEnabled ? "destructive-primary" : "primary"}
            >
              {isLoading
                ? currentlyEnabled
                  ? "Disabling..."
                  : "Enabling..."
                : currentlyEnabled
                  ? "Disable Server"
                  : targetIsPublic
                    ? "Enable & Make Public"
                    : "Enable Server"}
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
