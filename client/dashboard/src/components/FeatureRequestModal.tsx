import { Dialog } from "@/components/ui/dialog";
import { useTelemetry } from "@/contexts/Telemetry";
import { LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { ButtonRainbow } from "./ui/button-rainbow";
import { useRoutes } from "@/routes";

interface FeatureRequestModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description: string;
  actionType: string;
  icon?: LucideIcon;
  telemetryData?: Record<string, unknown>;
  accountUpgrade?: boolean;
}

export function FeatureRequestModal({
  isOpen,
  onClose,
  title,
  description,
  actionType,
  icon: Icon,
  telemetryData,
  accountUpgrade,
}: FeatureRequestModalProps) {
  const telemetry = useTelemetry();
  const routes = useRoutes();

  const handleRequestFeature = async () => {
    if (accountUpgrade) return; // For account upgrades, this is handled by the anchor tag's onClick

    try {
      telemetry.capture("feature_requested", {
        action: actionType,
        ...telemetryData,
      });
      toast.success("Feature requested");
      onClose();
    } catch {
      toast.error("Failed to request feature");
    }
  };

  const handleAccountUpgradeClick = async () => {
    telemetry.capture("feature_requested", {
      action: actionType,
      ...telemetryData,
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header className="text-center">
          {Icon && (
            <div className="mx-auto mb-4 flex h-20 w-20 items-center justify-center rounded-full bg-muted">
              <Icon className="h-10 w-10 text-muted-foreground" />
            </div>
          )}
          <Dialog.Title className="text-center">{title}</Dialog.Title>
          <Dialog.Description className="text-center">
            {description}
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer className="gap-3 sm:justify-center">
          {accountUpgrade ? (
            <ButtonRainbow
              href={routes.billing.href()}
              onClick={handleAccountUpgradeClick}
            >
              Upgrade
            </ButtonRainbow>
          ) : (
            <ButtonRainbow onClick={handleRequestFeature}>
              Request Feature
            </ButtonRainbow>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
