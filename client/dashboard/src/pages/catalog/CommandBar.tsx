import { Type } from "@/components/ui/type";
import { Plus, X } from "lucide-react";
import {
  AnimatePresence,
  motion,
  useMotionValue,
  useSpring,
  useTransform,
} from "motion/react";
import { useEffect, useRef, useState } from "react";

interface CommandBarProps {
  selectedCount: number;
  onAdd: () => void;
  onClear: () => void;
  anchorElement: HTMLElement | null;
  containerElement: HTMLElement | null;
}

export function CommandBar({
  selectedCount,
  onAdd,
  onClear,
  anchorElement,
  containerElement,
}: CommandBarProps) {
  const [isAnchorVisible, setIsAnchorVisible] = useState(true);
  const wasAnchorVisible = useRef(true);
  const [containerCenter, setContainerCenter] = useState<number | null>(null);

  // Motion values for smooth animation
  const rawTop = useMotionValue(0);
  const smoothTop = useSpring(rawTop, {
    stiffness: 300,
    damping: 30,
    mass: 0.8,
  });

  // Scale for genie effect - slightly smaller when fixed at bottom
  const scale = useTransform(smoothTop, (top) => {
    const viewportHeight = window.innerHeight;
    const fixedTop = viewportHeight - 72;
    // Scale down slightly as it approaches fixed position
    const progress = Math.min(1, Math.max(0, (top - fixedTop + 100) / 100));
    return 0.95 + 0.05 * progress;
  });

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

  // Use IntersectionObserver to detect when anchor leaves viewport
  useEffect(() => {
    if (!anchorElement || selectedCount === 0) {
      setIsAnchorVisible(true);
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        setIsAnchorVisible(entries[0].isIntersecting);
      },
      { threshold: 0 },
    );

    observer.observe(anchorElement);
    return () => observer.disconnect();
  }, [anchorElement, selectedCount]);

  // Track position and update motion value
  useEffect(() => {
    if (selectedCount === 0) return;

    let rafId: number;
    const barHeight = 48;
    const gap = 12;

    const updatePosition = () => {
      const viewportHeight = window.innerHeight;
      const fixedTop = viewportHeight - barHeight - 24;

      let targetTop: number;

      if (!anchorElement || !isAnchorVisible) {
        targetTop = fixedTop;
      } else {
        const rect = anchorElement.getBoundingClientRect();
        const idealTop = rect.bottom + gap;
        targetTop = Math.min(idealTop, fixedTop);
      }

      // Check if we're transitioning between states
      const isTransitioning = wasAnchorVisible.current !== isAnchorVisible;
      wasAnchorVisible.current = isAnchorVisible;

      if (isTransitioning) {
        // Let the spring animate smoothly
        rawTop.set(targetTop);
      } else if (isAnchorVisible && anchorElement) {
        // When following anchor, update immediately
        rawTop.jump(targetTop);
      } else {
        // Fixed position
        rawTop.set(targetTop);
      }

      rafId = requestAnimationFrame(updatePosition);
    };

    // Initialize position
    const viewportHeight = window.innerHeight;
    const fixedTop = viewportHeight - barHeight - 24;
    if (anchorElement && isAnchorVisible) {
      const rect = anchorElement.getBoundingClientRect();
      rawTop.jump(Math.min(rect.bottom + gap, fixedTop));
    } else {
      rawTop.jump(fixedTop);
    }

    updatePosition();
    return () => cancelAnimationFrame(rafId);
  }, [anchorElement, selectedCount, isAnchorVisible, rawTop]);

  // Default to viewport center if no container
  const leftPosition = containerCenter ?? window.innerWidth / 2;

  return (
    <AnimatePresence>
      {selectedCount > 0 && (
        <motion.div
          className="z-50 fixed"
          style={{
            top: smoothTop,
            left: leftPosition,
            x: "-50%",
          }}
        >
          <motion.div
            style={{ scale }}
            initial={{ opacity: 0, scale: 0.9, y: 20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.9, y: 20 }}
            transition={{ duration: 0.2, ease: "easeOut" }}
          >
            <div className="bg-background border rounded-2xl shadow-2xl px-3 py-2 flex items-center gap-3">
              {/* Count */}
              <Type small className="text-foreground font-medium pl-1">
                {selectedCount} selected
              </Type>

              {/* Divider */}
              <div className="w-px h-5 bg-border" />

              {/* Add button */}
              <button
                onClick={onAdd}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-full border border-accent bg-transparent text-secondary-foreground text-sm font-medium hover:bg-accent transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
                {selectedCount === 1 ? "Add" : "Add all"}
              </button>

              {/* Divider */}
              <div className="w-px h-5 bg-border" />

              {/* Clear button */}
              <button
                onClick={onClear}
                className="p-1.5 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                aria-label="Clear selection"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
