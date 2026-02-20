import { Dialog } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useProductTier } from "@/hooks/useProductTier";
import { useRoutes } from "@/routes";
import { FeatureName } from "@gram/client/models/components";
import { useFeaturesSetMutation, useGetPeriodUsage } from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { BarChart3, CreditCard, Server } from "lucide-react";
import { useEffect, useState } from "react";

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
  const productTier = useProductTier();
  const routes = useRoutes();
  const { data: periodUsage } = useGetPeriodUsage();
  const [enableInsights, setEnableInsights] = useState(true);
  const { mutate: setLogsFeature } = useFeaturesSetMutation();

  // Reset toggle to default ON when dialog opens
  useEffect(() => {
    if (isOpen) {
      setEnableInsights(true);
    }
  }, [isOpen]);

  const isFirstServerEnable =
    !currentlyEnabled && periodUsage?.actualEnabledServerCount === 0;

  const handleConfirm = () => {
    if (isFirstServerEnable && enableInsights) {
      setLogsFeature({
        request: {
          setProductFeatureRequestBody: {
            featureName: FeatureName.Logs,
            enabled: true,
          },
        },
      });
    }
    onConfirm();
    onClose();
  };

  const handleUpgrade = () => {
    routes.billing.goTo();
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
          {isFirstServerEnable && canEnable && (
            <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-muted/30 p-3">
              <div className="flex items-center gap-3">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary/10">
                  <BarChart3 className="h-4 w-4 text-primary" />
                </div>
                <div>
                  <Type className="text-sm font-medium">Enable Insights</Type>
                  <Type className="text-xs text-muted-foreground">
                    Capture requests and analytics
                  </Type>
                </div>
              </div>
              <Switch
                checked={enableInsights}
                onCheckedChange={setEnableInsights}
                aria-label="Enable Insights"
              />
            </div>
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
