import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { CreditCard, Server } from "lucide-react";

interface ServerEnableDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  currentlyEnabled?: boolean;
}

export function ServerEnableDialog({
  isOpen,
  onClose,
  onConfirm,
  isLoading = false,
  currentlyEnabled = false,
}: ServerEnableDialogProps) {
  const session = useSession();
  const routes = useRoutes();
  const { data: periodUsage } = useGetPeriodUsage();

  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  const handleUpgrade = () => {
    routes.billing.goTo();
    onClose();
  };

  // Specifying that the account is not just in "free" tier, but also that it has no subscription on file to pay for overages
  const isUnpaidAccount = session.gramAccountType === "free" && periodUsage && periodUsage.hasActiveSubscription === false;
  const hasAdditionalIncludedServers = periodUsage && periodUsage.includedServers > periodUsage.actualEnabledServerCount;

  const canEnable =
    isUnpaidAccount && !currentlyEnabled
      ? hasAdditionalIncludedServers
      : true;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title className="flex items-center gap-2">
            <Server className="w-5 h-5" />
            {currentlyEnabled ? "Disable" : "Enable"} MCP Server
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
              <CreditCard className="w-4 h-4" />
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
                  : "Enable Server"}
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
