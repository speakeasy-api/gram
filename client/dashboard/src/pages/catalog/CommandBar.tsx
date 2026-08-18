import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { Plus, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";

interface CommandBarProps {
  selectedCount: number;
  onAdd: () => void;
  onClear: () => void;
  containerElement: HTMLElement | null;
}

export function CommandBar({
  selectedCount,
  onAdd,
  onClear,
  containerElement,
}: CommandBarProps): JSX.Element {
  const barRef = useRef<HTMLDivElement>(null);
  const [containerCenter, setContainerCenter] = useState<number | null>(null);

  // Track container center for horizontal positioning
  useEffect(() => {
    if (!containerElement) {
      setContainerCenter(null);
      return;
    }

    const updateCenter = () => {
      const rect = containerElement.getBoundingClientRect();
      setContainerCenter(rect.left + rect.width / 2);
    };

    updateCenter();
    window.addEventListener("resize", updateCenter);
    return () => window.removeEventListener("resize", updateCenter);
  }, [containerElement]);

  // Default to viewport center if no container
  const leftPosition = containerCenter ?? window.innerWidth / 2;

  return (
    <AnimatePresence>
      {selectedCount > 0 && (
        <motion.div
          className="fixed bottom-14 z-50"
          style={{ left: leftPosition, x: "-50%" }}
          initial={{ opacity: 0, y: 40 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 20 }}
          transition={{ duration: 0.2, ease: "easeOut" }}
        >
          {/* Flat floating bar */}
          <div
            ref={barRef}
            data-command-bar
            className="border-border bg-card flex items-center gap-4 border px-4 py-3 shadow-md"
          >
            {/* Clear button */}
            <SimpleTooltip tooltip="Clear selection">
              <button
                onClick={onClear}
                className="text-muted-foreground hover:bg-muted hover:text-foreground p-1.5 transition-colors"
                aria-label="Clear selection"
              >
                <X className="h-4 w-4" />
              </button>
            </SimpleTooltip>

            {/* Divider */}
            <div className="bg-border h-5 w-px" />

            {/* Count */}
            <Text small className="font-medium">
              {selectedCount} {selectedCount === 1 ? "server" : "servers"}{" "}
              selected
            </Text>

            {/* Divider */}
            <div className="bg-border h-5 w-px" />

            {/* Add button - min-w to prevent layout shift */}
            <button
              onClick={onAdd}
              className="bg-foreground text-background hover:bg-foreground/90 flex min-w-[5.5rem] items-center justify-center gap-1.5 px-3 py-1.5 text-sm font-medium transition-colors"
            >
              <Plus className="h-3.5 w-3.5" />
              Add to project
            </button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
