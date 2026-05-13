import { useEffect, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { cn } from "@/lib/utils";

/**
 * EditModal mounts a motion.div that shares its `layoutId` with the originating
 * card's motion.div. When mounted, motion runs a layout transition from the
 * card's last bounding rect to the modal's target rect — that's the "grow
 * from origin" effect. Closing reverses it.
 *
 * Backdrop is a transparent click-catcher (no fade/blur). Footer is rendered
 * sticky at the bottom of the modal box; body scrolls between header and
 * footer.
 */
export function EditModal({
  layoutId,
  open,
  onClose,
  title,
  footer,
  level = 0,
  children,
}: {
  layoutId: string;
  open: boolean;
  onClose: () => void;
  title?: ReactNode;
  footer?: ReactNode;
  /** Bumps z-index for nested modals. Default 0. */
  level?: number;
  children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const backdropZ = 40 + level * 20;
  const shellZ = 50 + level * 20;

  return (
    <AnimatePresence>
      {open && (
        <>
          <div
            key="backdrop"
            className="fixed inset-0"
            style={{ zIndex: backdropZ }}
            onClick={onClose}
          />
          <motion.div
            key="shell"
            layoutId={layoutId}
            transition={{
              type: "spring",
              stiffness: 320,
              damping: 32,
              mass: 0.6,
            }}
            style={{ zIndex: shellZ }}
            className={cn(
              "fixed top-1/2 left-1/2",
              "-translate-x-1/2 -translate-y-1/2",
              "w-[min(34rem,calc(100vw-2rem))] max-h-[85vh]",
              "bg-card text-card-foreground border border-border rounded-md shadow-xl",
              "overflow-hidden flex flex-col",
            )}
          >
            <motion.div
              className="flex flex-col min-h-0 flex-1"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ delay: 0.15, duration: 0.15 }}
            >
              {title && (
                <header className="px-6 pt-5 pb-3">
                  <div className="min-w-0">{title}</div>
                </header>
              )}
              <div className="flex-1 overflow-y-auto px-6 pb-6">{children}</div>
              {footer && (
                <footer className="border-t border-border bg-card px-6 py-3">
                  {footer}
                </footer>
              )}
            </motion.div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
