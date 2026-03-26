import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Plus, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";

function LiquidGlassFilter() {
  return (
    <svg style={{ display: "none" }}>
      <filter
        id="command-bar-glass"
        x="0%"
        y="0%"
        width="100%"
        height="100%"
        filterUnits="objectBoundingBox"
      >
        <feTurbulence
          type="fractalNoise"
          baseFrequency="0.02 0.02"
          numOctaves="1"
          seed="5"
          result="turbulence"
        />
        <feComponentTransfer in="turbulence" result="mapped">
          <feFuncR type="gamma" amplitude="1" exponent="10" offset="0.5" />
          <feFuncG type="gamma" amplitude="0" exponent="1" offset="0" />
          <feFuncB type="gamma" amplitude="0" exponent="1" offset="0.5" />
        </feComponentTransfer>
        <feGaussianBlur in="turbulence" stdDeviation="3" result="softMap" />
        <feSpecularLighting
          in="softMap"
          surfaceScale="3"
          specularConstant="0.8"
          specularExponent="80"
          lightingColor="white"
          result="specLight"
        >
          <fePointLight x="-100" y="-100" z="200" />
        </feSpecularLighting>
        <feComposite
          in="specLight"
          operator="arithmetic"
          k1="0"
          k2="1"
          k3="1"
          k4="0"
          result="litImage"
        />
        <feDisplacementMap
          in="SourceGraphic"
          in2="softMap"
          scale="60"
          xChannelSelector="R"
          yChannelSelector="G"
        />
      </filter>
    </svg>
  );
}

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
}: CommandBarProps) {
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
          className="z-50 fixed bottom-14"
          style={{ left: leftPosition, x: "-50%" }}
          initial={{ opacity: 0, scale: 0.8, y: 40 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.9, y: 20 }}
          transition={{
            duration: 0.4,
            ease: [0.34, 1.56, 0.64, 1], // Bouncy spring effect
          }}
        >
          {/* Liquid Glass Container */}
          <div
            ref={barRef}
            data-command-bar
            className="relative rounded-2xl overflow-hidden border border-primary/20 dark:border-white/10 shadow-[0_8px_32px_rgba(0,0,0,0.25),0_0_40px_rgba(59,130,246,0.2),0_0_80px_rgba(59,130,246,0.1)] dark:shadow-none"
          >
            {/* Glass effect layer - blur + distortion */}
            <div
              className="absolute inset-0 backdrop-blur-sm"
              style={{ filter: "url(#command-bar-glass)" }}
            />
            {/* Tint layer - opaque enough for text readability */}
            <div className="absolute inset-0 bg-white/80 dark:bg-black/70" />
            {/* Shine layer - light mode only */}
            <div className="absolute inset-0 dark:hidden shadow-[inset_2px_2px_1px_0_rgba(255,255,255,0.5),inset_-1px_-1px_1px_1px_rgba(255,255,255,0.3)]" />
            {/* Content layer */}
            <div className="relative z-10 px-4 py-3 flex items-center gap-4">
              {/* Clear button */}
              <SimpleTooltip tooltip="Clear selection">
                <button
                  onClick={onClear}
                  className="p-1.5 rounded-full text-black/50 hover:text-black hover:bg-black/10 dark:text-white/50 dark:hover:text-white dark:hover:bg-white/10 transition-colors"
                  aria-label="Clear selection"
                >
                  <X className="w-4 h-4" />
                </button>
              </SimpleTooltip>

              {/* Divider */}
              <div className="w-px h-5 bg-black/20 dark:bg-white/20" />

              {/* Count */}
              <Type small className="font-medium text-black dark:text-white">
                {selectedCount} {selectedCount === 1 ? "server" : "servers"}{" "}
                selected
              </Type>

              {/* Divider */}
              <div className="w-px h-5 bg-black/20 dark:bg-white/20" />

              {/* Add button - min-w to prevent layout shift */}
              <button
                onClick={onAdd}
                className="flex items-center justify-center gap-1.5 min-w-[5.5rem] px-3 py-1.5 rounded-full text-sm font-medium bg-foreground text-background hover:bg-foreground/90 transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
                Add to project
              </button>
            </div>
          </div>
          <LiquidGlassFilter />
        </motion.div>
      )}
    </AnimatePresence>
  );
}
