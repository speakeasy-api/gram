import { motion, useReducedMotion } from "motion/react";
import { useLayoutEffect, useRef, useState } from "react";

const GROW = { type: "spring", stiffness: 380, damping: 38 } as const;

/**
 * A region that follows its content's height with a spring instead of
 * jumping, so whatever sits below it in the flow slides rather than snaps.
 * Clips only while it moves, so focus rings and popovers inside are never
 * cut off at rest. Under reduced motion it resizes at once.
 */
export function AutoHeight({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  const reduced = useReducedMotion() ?? false;
  const inner = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | "auto">("auto");
  const [moving, setMoving] = useState(false);

  useLayoutEffect(() => {
    const element = inner.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      setHeight(entry?.borderBoxSize?.[0]?.blockSize ?? element.offsetHeight);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return (
    <motion.div
      initial={false}
      animate={{ height }}
      transition={reduced ? { duration: 0 } : GROW}
      onAnimationStart={() => setMoving(true)}
      onAnimationComplete={() => setMoving(false)}
      style={{ overflow: moving ? "hidden" : "visible" }}
    >
      <div ref={inner} className={className}>
        {children}
      </div>
    </motion.div>
  );
}
