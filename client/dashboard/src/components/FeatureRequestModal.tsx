import { Dialog } from "@/components/ui/Dialog";
import { InputField } from "@/components/moon/input-field";
import { useTelemetry } from "@/contexts/Telemetry";
import { Button } from "@/components/ui/Button";
import { LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { useOrgRoutes } from "@/routes";

interface FeatureRequestInput {
  label: string;
  placeholder: string;
  telemetryField: string;
}

interface FeatureRequestModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description: string;
  actionType: string;
  icon?: LucideIcon;
  telemetryData?: Record<string, unknown>;
  accountUpgrade?: boolean;
  requestInput?: FeatureRequestInput;
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
  requestInput,
}: FeatureRequestModalProps): JSX.Element {
  const telemetry = useTelemetry();
  const routes = useOrgRoutes();
  const [requestInputValue, setRequestInputValue] = useState("");

  useEffect(() => {
    if (!isOpen) {
      setRequestInputValue("");
    }
  }, [isOpen]);

  const handleClose = () => {
    setRequestInputValue("");
    onClose();
  };

  const handleRequestFeature = async () => {
    if (accountUpgrade) return; // For account upgrades, this is handled by the anchor tag's onClick

    try {
      const requestTelemetry = { ...telemetryData };
      if (requestInput) {
        requestTelemetry[requestInput.telemetryField] =
          requestInputValue.trim();
      }
      telemetry.capture("feature_requested", {
        action: actionType,
        ...requestTelemetry,
      });
      toast.success("Feature requested");
      handleClose();
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
    <Dialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open) handleClose();
      }}
    >
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header className="text-center">
          {Icon && (
            <div className="bg-muted mx-auto mb-4 flex h-20 w-20 items-center justify-center rounded-full">
              <Icon className="text-muted-foreground h-10 w-10" />
            </div>
          )}
          <Dialog.Title className="text-center">{title}</Dialog.Title>
          <Dialog.Description className="text-center">
            {description}
          </Dialog.Description>
        </Dialog.Header>
        {requestInput && (
          <InputField
            label={requestInput.label}
            value={requestInputValue}
            onChange={(event) => setRequestInputValue(event.target.value)}
            placeholder={requestInput.placeholder}
            required
            autoFocus
          />
        )}
        <Dialog.Footer className="gap-3 sm:justify-center">
          {accountUpgrade ? (
            <Button
              variant="brand"
              onClick={() => {
                void handleAccountUpgradeClick();
                window.open(routes.billing.href(), "_self");
              }}
            >
              UPGRADE
            </Button>
          ) : (
            <Button
              variant="brand"
              disabled={!!requestInput && !requestInputValue.trim()}
              onClick={() => void handleRequestFeature()}
            >
              REQUEST FEATURE
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
