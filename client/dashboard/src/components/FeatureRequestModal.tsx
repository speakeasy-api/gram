import { Dialog } from "@/components/ui/dialog";
import { useTelemetry } from "@/contexts/Telemetry";
import { toast } from "sonner";
import { useState } from "react";
import { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

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
  const [isRequesting, setIsRequesting] = useState(false);

  const handleRequestFeature = async () => {
    if (accountUpgrade) return; // For account upgrades, this is handled by the anchor tag's onClick

    setIsRequesting(true);
    try {
      telemetry.capture("feature_requested", {
        action: actionType,
        ...telemetryData,
      });
      toast.success("Feature requested");
      onClose();
    } catch {
      toast.error("Failed to request feature");
    } finally {
      setIsRequesting(false);
    }
  };

  const handleAccountUpgradeClick = () => {
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
            <div
              className={cn(
                "inline-block rounded-md p-[1px]",
                "bg-gradient-primary",
                isRequesting && "opacity-50"
              )}
            >
              <a
                href="https://calendly.com/sagar-speakeasy/30min"
                target="_blank"
                rel="noopener noreferrer"
                onClick={handleAccountUpgradeClick}
                className={cn(
                  "relative inline-flex items-center justify-center gap-2 px-4 py-2",
                  "font-mono text-sm uppercase text-foreground",
                  "rounded-md cursor-pointer",
                  "transition-all outline-none",
                  "w-full rounded-[7px] bg-background border-0",
                  "hover:bg-background/95",
                  "focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-neutral-500"
                )}
              >
                Book Meeting
              </a>
            </div>
          ) : (
            <div
              className={cn(
                "inline-block rounded-md p-[1px]",
                "bg-gradient-primary",
                isRequesting && "opacity-50"
              )}
            >
              <button
                disabled={isRequesting}
                onClick={handleRequestFeature}
                className={cn(
                  "relative inline-flex items-center justify-center gap-2 px-4 py-2",
                  "font-mono text-sm uppercase text-foreground",
                  "rounded-md cursor-pointer",
                  "transition-all outline-none",
                  "w-full rounded-[7px] bg-background border-0",
                  "hover:bg-background/95",
                  "disabled:cursor-not-allowed",
                  "focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-neutral-500"
                )}
              >
                {isRequesting ? "Requesting..." : "Request Feature"}
              </button>
            </div>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
