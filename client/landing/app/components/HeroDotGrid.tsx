"use client";

import { useEffect, useState } from "react";
import { motion, MotionValue, useSpring } from "framer-motion";

interface Dot {
  id: string;
  x: number;
  y: number;
  size: number;
  delay: number;
  row: number;
  col: number;
}

interface DotComponentProps {
  dot: Dot;
  active: { row: number; col: number };
  setActive: (active: { row: number; col: number }) => void;
  dragX: MotionValue<number>;
  dragY: MotionValue<number>;
  allDots: Dot[];
}

interface HeroDotGridProps {
  isResizing: boolean;
  active: { row: number; col: number };
  setActive: (active: { row: number; col: number }) => void;
  dragX: MotionValue<number>;
  dragY: MotionValue<number>;
  introducingRef: React.RefObject<HTMLHeadingElement | null>;
  gramRef: React.RefObject<HTMLHeadingElement | null>;
  mcpTextRef: React.RefObject<HTMLDivElement | null>;
  descriptionRef: React.RefObject<HTMLDivElement | null>;
  buttonsRef: React.RefObject<HTMLDivElement | null>;
}

const distance = (
  pointA: { x: number; y: number },
  pointB: { x: number; y: number }
) => {
  const xDiff = pointB.x - pointA.x;
  const yDiff = pointB.y - pointA.y;
  return Math.sqrt(xDiff * xDiff + yDiff * yDiff);
};

const shouldSkipDot = (
  x: number,
  y: number,
  introducingBounds: DOMRect,
  gramBounds: DOMRect,
  mcpTextBounds: DOMRect | null,
  descriptionBounds: DOMRect,
  buttonsBounds: DOMRect | null,
  isMobile: boolean,
  isTablet: boolean
) => {
  const introducingPadding = isMobile ? 20 : isTablet ? 25 : 30;
  const introducingDescenderExtra = isMobile ? 15 : isTablet ? 20 : 25;

  if (
    x >= introducingBounds.left - introducingPadding &&
    x <= introducingBounds.right + introducingPadding &&
    y >= introducingBounds.top - introducingPadding &&
    y <= introducingBounds.bottom + introducingDescenderExtra
  ) {
    return true;
  }

  const gramPadding = isMobile ? 20 : isTablet ? 25 : 30;
  const gramDescenderExtra = isMobile ? 25 : isTablet ? 30 : 40;

  if (
    x >= gramBounds.left - gramPadding &&
    x <= gramBounds.right + gramPadding &&
    y >= gramBounds.top - gramPadding &&
    y <= gramBounds.bottom + gramDescenderExtra
  ) {
    return true;
  }

  if (mcpTextBounds) {
    const mcpPadding = isMobile ? 25 : isTablet ? 35 : 45;
    if (
      x >= mcpTextBounds.left - mcpPadding &&
      x <= mcpTextBounds.right + mcpPadding &&
      y >= mcpTextBounds.top - mcpPadding &&
      y <= mcpTextBounds.bottom + mcpPadding
    ) {
      return true;
    }
  }

  const descPadding = isMobile ? 25 : isTablet ? 35 : 45;
  if (
    x >= descriptionBounds.left - descPadding &&
    x <= descriptionBounds.right + descPadding &&
    y >= descriptionBounds.top - descPadding &&
    y <= descriptionBounds.bottom + descPadding
  ) {
    return true;
  }

  if (buttonsBounds) {
    const buttonPadding = isMobile ? 50 : isTablet ? 70 : 90;
    if (
      x >= buttonsBounds.left - buttonPadding &&
      x <= buttonsBounds.right + buttonPadding &&
      y >= buttonsBounds.top - buttonPadding &&
      y <= buttonsBounds.bottom + buttonPadding
    ) {
      return true;
    }
  }

  return false;
};

const DotComponent = ({
  dot,
  active,
  setActive,
  dragX,
  dragY,
  allDots,
}: DotComponentProps) => {
  const isDragging = dot.col === active.col && dot.row === active.row;
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < 768);
    };

    checkMobile();
    window.addEventListener("resize", checkMobile);
    return () => window.removeEventListener("resize", checkMobile);
  }, []);

  const activeDot = allDots.find(
    (d) => d.row === active.row && d.col === active.col
  );
  const d = activeDot
    ? distance({ x: activeDot.x, y: activeDot.y }, { x: dot.x, y: dot.y })
    : 0;

  const maxDistance = 2000;
  const normalizedDistance = d / maxDistance;

  const falloff = Math.exp(-normalizedDistance * 2);

  const springConfig = {
    stiffness: 100 + falloff * 600,
    damping: 20 + (1 - falloff) * 40,
  };

  const dx = useSpring(dragX || 0, springConfig);
  const dy = useSpring(dragY || 0, springConfig);

  return (
    <motion.div
      drag={!isMobile}
      dragConstraints={{ left: 0, right: 0, top: 0, bottom: 0 }}
      dragTransition={{ bounceStiffness: 500, bounceDamping: 20 }}
      dragElastic={1}
      onDragStart={() => setActive({ row: dot.row, col: dot.col })}
      onDrag={(_, info) => {
        dragX.set(info.offset.x);
        dragY.set(info.offset.y);
      }}
      onDragEnd={() => {
        setActive({ row: -1, col: -1 });
        dragX.set(0);
        dragY.set(0);
      }}
      className={`absolute ${
        isMobile ? "" : "cursor-grab active:cursor-grabbing"
      } select-none`}
      style={{
        width: dot.size,
        height: dot.size,
        left: dot.x,
        top: dot.y,
        x: isDragging && dragX ? dragX : dx,
        y: isDragging && dragY ? dragY : dy,
        translateX: "-50%",
        translateY: "-50%",
        zIndex: isDragging ? 10 : 1,
        pointerEvents: isMobile ? "none" : "auto",
      }}
      initial={{ opacity: 0, scale: 0.5 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{
        duration: 0.6,
        delay: dot.delay,
        ease: "easeOut",
      }}
      whileTap={isMobile ? { scale: 1.2 } : {}}
    >
      <svg
        width={dot.size}
        height={dot.size}
        viewBox={`0 0 ${dot.size} ${dot.size}`}
        fill="none"
        className="transition-all duration-300"
      >
        <defs>
          <linearGradient
            id={`gradient-${dot.id}`}
            x1="0"
            y1="0"
            x2={dot.size}
            y2="0"
            gradientUnits="userSpaceOnUse"
          >
            <stop stopOpacity="0" />
            <stop offset="1" stopOpacity="0.2" />
          </linearGradient>
        </defs>
        <circle
          cx={dot.size / 2}
          cy={dot.size / 2}
          r={dot.size / 2 - 0.5}
          fill="white"
          stroke={`url(#gradient-${dot.id})`}
          strokeWidth="1"
          className="transition-all duration-200"
        />
      </svg>
    </motion.div>
  );
};

export default function HeroDotGrid({
  isResizing,
  active,
  setActive,
  dragX,
  dragY,
  introducingRef,
  gramRef,
  // mcpTextRef,
  descriptionRef,
  buttonsRef,
}: HeroDotGridProps) {
  const [dots, setDots] = useState<Dot[]>([]);

  const generateDotGrid = () => {
    const container = document.getElementById("dotGrid");
    if (!container) return;

    const isMobile = window.innerWidth < 768;
    const isTablet = window.innerWidth >= 768 && window.innerWidth < 1024;

    const containerBounds = container.getBoundingClientRect();
    const introducingBounds = introducingRef.current?.getBoundingClientRect();
    const gramBounds = gramRef.current?.getBoundingClientRect();
    // const mcpTextBounds = mcpTextRef.current?.getBoundingClientRect();
    const descriptionBounds = descriptionRef.current?.getBoundingClientRect();
    const buttonsBounds = buttonsRef.current?.getBoundingClientRect();

    if (!introducingBounds || !gramBounds || !descriptionBounds) {
      setTimeout(generateDotGrid, 50);
      return;
    }

    const dotSize = isMobile ? 28 : isTablet ? 32 : 40;
    const dotSpacing = isMobile ? 36 : isTablet ? 40 : 55;

    const screenWidth = window.innerWidth;

    const paddingX = isMobile ? 24 : isTablet ? 40 : 160;

    const startX = paddingX;
    const startY = Math.max(0, introducingBounds.top - containerBounds.top);
    const endX = screenWidth - (isMobile ? paddingX : screenWidth * 0.08);
    const endY = containerBounds.height - (isMobile ? 120 : 80);

    const cols = Math.ceil((endX - startX) / dotSpacing);
    const rows = Math.ceil((endY - startY) / (dotSpacing * 0.87));

    // Convert bounds to container-relative coordinates
    const relativeIntroducingBounds = {
      left: introducingBounds.left - containerBounds.left,
      right: introducingBounds.right - containerBounds.left,
      top: introducingBounds.top - containerBounds.top,
      bottom: introducingBounds.bottom - containerBounds.top,
    };

    const relativeGramBounds = {
      left: gramBounds.left - containerBounds.left,
      right: gramBounds.right - containerBounds.left,
      top: gramBounds.top - containerBounds.top,
      bottom: gramBounds.bottom - containerBounds.top,
    };

    // const relativeMcpTextBounds = mcpTextBounds
    //   ? {
    //       left: mcpTextBounds.left - containerBounds.left,
    //       right: mcpTextBounds.right - containerBounds.left,
    //       top: mcpTextBounds.top - containerBounds.top,
    //       bottom: mcpTextBounds.bottom - containerBounds.top,
    //     }
    //   : null;

    const relativeDescriptionBounds = {
      left: descriptionBounds.left - containerBounds.left,
      right: descriptionBounds.right - containerBounds.left,
      top: descriptionBounds.top - containerBounds.top,
      bottom: descriptionBounds.bottom - containerBounds.top,
    };

    const relativeButtonsBounds = buttonsBounds
      ? {
          left: buttonsBounds.left - containerBounds.left,
          right: buttonsBounds.right - containerBounds.left,
          top: buttonsBounds.top - containerBounds.top,
          bottom: buttonsBounds.bottom - containerBounds.top,
        }
      : null;

    const newDots = [];

    for (let row = 0; row < rows; row++) {
      for (let col = 0; col < cols; col++) {
        const xOffset = row % 2 === 0 ? 0 : dotSpacing / 2;
        const x = startX + col * dotSpacing + xOffset;
        const y = startY + row * dotSpacing * 0.87;

        if (x < startX || x > endX || y < startY || y > endY) {
          continue;
        }

        if (isMobile && row % 2 === 0 && col % 2 === 0) {
          continue;
        }

        if (
          y < relativeIntroducingBounds.bottom &&
          x < relativeIntroducingBounds.right + 20
        ) {
          continue;
        }

        if (
          shouldSkipDot(
            x,
            y,
            relativeIntroducingBounds as DOMRect,
            relativeGramBounds as DOMRect,
            null,
            relativeDescriptionBounds as DOMRect,
            relativeButtonsBounds as DOMRect | null,
            isMobile,
            isTablet
          )
        ) {
          continue;
        }

        const centerX = startX + (endX - startX) / 2;
        const centerY = startY + (endY - startY) / 2;
        const dx = x - centerX;
        const dy = y - centerY;
        const distance = Math.sqrt(dx * dx + dy * dy);
        const delay = distance * 0.0003 + Math.random() * 0.1;

        newDots.push({
          id: `dot-${row}-${col}`,
          x,
          y,
          size: dotSize,
          delay,
          row,
          col,
        });
      }
    }

    setDots(newDots);
  };

  useEffect(() => {
    const handleResize = () => {
      generateDotGrid();
    };

    document.fonts.ready.then(() => {
      generateDotGrid();
    });

    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, [introducingRef, gramRef, descriptionRef, buttonsRef]);

  return (
    <div
      id="dotGrid"
      className={`absolute inset-0 overflow-hidden transition-opacity duration-300 ${
        isResizing ? "opacity-0" : "opacity-100"
      }`}
    >
      {dots.map((dot) => (
        <DotComponent
          key={dot.id}
          dot={dot}
          active={active}
          setActive={setActive}
          dragX={dragX}
          dragY={dragY}
          allDots={dots}
        />
      ))}
    </div>
  );
}
