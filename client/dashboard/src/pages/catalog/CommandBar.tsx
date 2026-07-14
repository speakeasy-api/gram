import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Button } from "@/components/ui/button";
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
            className="border-primary/20 relative overflow-hidden border"
          >
            {/* Glass effect layer - blur + distortion */}
            <div
              className="absolute inset-0 backdrop-blur-sm"
              style={{ filter: "url(#command-bar-glass)" }}
            />
            {/* Tint layer - opaque enough for text readability */}
            <div className="bg-background/80 absolute inset-0" />
            {/* Content layer */}
            <div className="relative z-10 flex items-center gap-4 px-4 py-3">
              {/* Clear button */}
              <SimpleTooltip tooltip="Clear selection">
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={onClear}
                  aria-label="Clear selection"
                  className="rounded-full p-1.5"
                >
                  <X className="h-4 w-4" />
                </Button>
              </SimpleTooltip>

              {/* Divider */}
              <div className="bg-border h-5 w-px" />

              {/* Count */}
              <Type small className="text-foreground font-medium">
                {selectedCount} {selectedCount === 1 ? "server" : "servers"}{" "}
                selected
              </Type>

              {/* Divider */}
              <div className="bg-border h-5 w-px" />

              {/* Add button - min-w to prevent layout shift */}
              <Button
                onClick={onAdd}
                size="sm"
                className="min-w-[5.5rem] rounded-full"
              >
                <Button.LeftIcon>
                  <Plus className="h-3.5 w-3.5" />
                </Button.LeftIcon>
                <Button.Text>Add to project</Button.Text>
              </Button>
            </div>
          </div>
          <LiquidGlassFilter />
        </motion.div>
      )}
    </AnimatePresence>
  );
}
