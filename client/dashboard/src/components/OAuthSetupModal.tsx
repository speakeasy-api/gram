import { Dialog } from "@/components/ui/dialog";
import { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface OAuthSetupModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description: string;
  icon?: LucideIcon;
  docsUrl: string;
  primaryAction: {
    label: string;
    href?: string;
    onClick?: () => void;
    isLoading?: boolean;
  };
}

export function OAuthSetupModal({
  isOpen,
  onClose,
  title,
  description,
  icon: Icon,
  docsUrl,
  primaryAction,
}: OAuthSetupModalProps) {
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
          <a
            href={docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              "inline-flex items-center justify-center gap-2 px-4 py-2",
              "font-mono text-sm uppercase",
              "border border-border rounded-md",
              "bg-background text-foreground",
              "hover:bg-accent hover:text-accent-foreground",
              "focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-neutral-500",
              "transition-colors outline-none"
            )}
          >
            View Setup Docs
          </a>
          <div
            className={cn(
              "inline-block rounded-md p-[1px]",
              "bg-gradient-primary",
              primaryAction.isLoading && "opacity-50"
            )}
          >
            {primaryAction.href ? (
              <a
                href={primaryAction.href}
                target="_blank"
                rel="noopener noreferrer"
                onClick={primaryAction.onClick}
                className={cn(
                  "relative inline-flex items-center justify-center gap-2 px-4 py-2",
                  "font-mono text-sm uppercase text-foreground",
                  "rounded-md cursor-pointer",
                  "transition-all outline-none",
                  "w-full rounded-[7px] bg-background border-0",
                  "hover:bg-background/95"
                )}
              >
                {primaryAction.label}
              </a>
            ) : (
              <button
                disabled={primaryAction.isLoading}
                onClick={primaryAction.onClick}
                autoFocus={false}
                className={cn(
                  "relative inline-flex items-center justify-center gap-2 px-4 py-2",
                  "font-mono text-sm uppercase text-foreground",
                  "rounded-md cursor-pointer",
                  "transition-all outline-none",
                  "w-full rounded-[7px] bg-background border-0",
                  "hover:bg-background/95",
                  "disabled:cursor-not-allowed"
                )}
              >
                {primaryAction.isLoading
                  ? "Requesting..."
                  : primaryAction.label}
              </button>
            )}
          </div>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
