import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
} from "@radix-ui/react-dialog";
import { useModal } from "@/components/ui/moonshine/hooks/useModal";
import { cn } from "@/components/ui/moonshine/lib/utils";
import { IconButton } from "@/components/ui/moonshine/components/IconButton";
import { Icon } from "../Icon";
import { Screen } from "@/components/ui/moonshine/context/ModalContext";

export interface ModalProps {
  closable?: boolean;
  className?: string;
  layout: "default" | "custom";
  onClose?: (currentScreen: Screen) => void;
}

export const Modal = ({
  closable = false,
  className,
  layout,
  onClose,
}: ModalProps): React.JSX.Element | null => {
  const { screens, currentIndex, isOpen, close } = useModal();
  const currentScreen = screens[currentIndex];

  const handleOpenChange = (open: boolean) => {
    if (closable && !open) {
      close();
      if (currentScreen) onClose?.(currentScreen);
    }
  };

  if (!isOpen) return null;
  if (!currentScreen) return null;

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogPortal>
        <DialogOverlay className="fixed top-0 z-10 h-screen w-screen bg-surface-secondary opacity-85" />

        {layout === "default" ? (
          <DialogContent
            className={cn(
              "fixed top-1/2 left-1/2 z-20 flex h-auto max-h-[85vh] min-h-[40vh] w-[90vw] max-w-[800px] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-y-auto rounded-md border-neutral-default bg-surface-primary p-10 shadow-lg outline-none",
              className,
            )}
          >
            {closable && (
              <DialogClose asChild>
                <IconButton
                  variant="tertiary"
                  icon={
                    <Icon
                      name="x"
                      className="text-neutral-default hover:text-neutral-default/80 size-7 focus:outline-none"
                    />
                  }
                  aria-label="Close modal"
                  className="absolute top-4 right-4 focus:ring-0 focus:outline-none focus-visible:ring-0"
                />
              </DialogClose>
            )}
            {currentScreen.title && (
              <DialogTitle className="text-display-sm">
                {currentScreen.title}
              </DialogTitle>
            )}
            <DialogDescription>{currentScreen.component}</DialogDescription>
          </DialogContent>
        ) : (
          <DialogContent
            className={cn(
              "fixed top-1/2 left-1/2 z-20 flex h-auto max-h-[85vh] min-h-[40vh] w-[90vw] max-w-[800px] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-y-auto rounded-md border-neutral-default bg-surface-primary p-10 shadow-lg outline-none",
              className,
            )}
          >
            {closable && (
              <DialogClose asChild>
                <IconButton
                  variant="tertiary"
                  icon={
                    <Icon
                      name="x"
                      className="text-neutral-default hover:text-neutral-default/80 size-7 focus:outline-none"
                    />
                  }
                  aria-label="Close modal"
                  className="absolute top-4 right-4 focus:ring-0 focus:outline-none focus-visible:ring-0"
                />
              </DialogClose>
            )}
            {currentScreen.component}
          </DialogContent>
        )}
      </DialogPortal>
    </Dialog>
  );
};
