"use client";

import { useEffect, useRef, useState } from "react";
import {
  motion,
  MotionValue,
  useMotionValue,
  useSpring,
} from "framer-motion";

interface Dot {
  id: string;
  x: number;
  y: number;
  size: number;
  delay: number;
  row: number;
  col: number;
}

const distance = (
  pointA: { x: number; y: number },
  pointB: { x: number; y: number }
) => {
  const xDiff = pointB.x - pointA.x;
  const yDiff = pointB.y - pointA.y;
  return Math.sqrt(xDiff * xDiff + yDiff * yDiff);
};

interface DotComponentProps {
  dot: Dot;
  active: { row: number; col: number };
  setActive: (active: { row: number; col: number }) => void;
  dragX: MotionValue<number>;
  dragY: MotionValue<number>;
  allDots: Dot[];
}

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

interface FooterDotsHeroLikeProps {
  footerHeadingRef: React.RefObject<HTMLHeadingElement | null>;
  footerDescRef: React.RefObject<HTMLParagraphElement | null>;
  footerButtonsRef: React.RefObject<HTMLDivElement | null>;
}

export default function FooterDotsHeroLike({
  footerHeadingRef,
  footerDescRef,
  footerButtonsRef,
}: FooterDotsHeroLikeProps) {
  const [dots, setDots] = useState<Dot[]>([]);
  const [isResizing, setIsResizing] = useState(false);
  const [active, setActive] = useState({ row: -1, col: -1 });
  const [isVisible, setIsVisible] = useState(false);
  const resizeTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const dragX = useMotionValue(0);
  const dragY = useMotionValue(0);

  const shouldSkipDot = (
    x: number,
    y: number,
    headingBounds: DOMRect,
    descBounds: DOMRect,
    buttonsBounds: DOMRect | null,
    isMobile: boolean,
    isTablet: boolean
  ) => {
    const headingPadding = isMobile ? 20 : isTablet ? 25 : 30;
    const headingDescenderExtra = isMobile ? 15 : isTablet ? 20 : 25;
    if (
      x >= headingBounds.left - headingPadding &&
      x <= headingBounds.right + headingPadding &&
      y >= headingBounds.top - headingPadding &&
      y <= headingBounds.bottom + headingDescenderExtra
    ) {
      return true;
    }
    const descPadding = isMobile ? 25 : isTablet ? 35 : 45;
    if (
      x >= descBounds.left - descPadding &&
      x <= descBounds.right + descPadding &&
      y >= descBounds.top - descPadding &&
      y <= descBounds.bottom + descPadding
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

  const generateDotGrid = () => {
    const container = containerRef.current;
    if (!container || !isVisible) return;
    const isMobile = window.innerWidth < 768;
    const isTablet = window.innerWidth >= 768 && window.innerWidth < 1024;

    const containerBounds = container.getBoundingClientRect();

    const headingBounds = footerHeadingRef.current?.getBoundingClientRect();
    const descBounds = footerDescRef.current?.getBoundingClientRect();
    const buttonsBounds = footerButtonsRef.current?.getBoundingClientRect();
    if (!headingBounds || !descBounds) {
      setTimeout(generateDotGrid, 50);
      return;
    }

    const relativeHeadingBounds = {
      left: headingBounds.left - containerBounds.left,
      right: headingBounds.right - containerBounds.left,
      top: headingBounds.top - containerBounds.top,
      bottom: headingBounds.bottom - containerBounds.top,
    };

    const relativeDescBounds = {
      left: descBounds.left - containerBounds.left,
      right: descBounds.right - containerBounds.left,
      top: descBounds.top - containerBounds.top,
      bottom: descBounds.bottom - containerBounds.top,
    };

    const relativeButtonsBounds = buttonsBounds
      ? {
          left: buttonsBounds.left - containerBounds.left,
          right: buttonsBounds.right - containerBounds.left,
          top: buttonsBounds.top - containerBounds.top,
          bottom: buttonsBounds.bottom - containerBounds.top,
        }
      : null;

    const dotSize = isMobile ? 28 : isTablet ? 32 : 40;
    const dotSpacing = isMobile ? 36 : isTablet ? 40 : 55;

    const containerWidth = containerBounds.width;
    const containerHeight = containerBounds.height;

    const paddingX = isMobile ? 24 : isTablet ? 40 : 160;
    const startX = paddingX;
    const startY = isMobile ? 60 : 80;
    const endX = containerWidth - paddingX;
    const endY = containerHeight - (isMobile ? 60 : 80);

    const cols = Math.ceil((endX - startX) / dotSpacing);
    const rows = Math.ceil((endY - startY) / (dotSpacing * 0.87));
    const newDots = [];

    for (let row = 0; row < rows; row++) {
      for (let col = 0; col < cols; col++) {
        const xOffset = row % 2 === 0 ? 0 : dotSpacing / 2;
        const x = startX + col * dotSpacing + xOffset;
        const y = startY + row * dotSpacing * 0.87;

        const dotRadius = dotSize / 2;
        if (
          x - dotRadius < 0 ||
          x + dotRadius > containerWidth ||
          y - dotRadius < 0 ||
          y + dotRadius > containerHeight
        ) {
          continue;
        }

        if (isMobile && row % 2 === 0 && col % 2 === 0) {
          continue;
        }

        if (
          shouldSkipDot(
            x,
            y,
            relativeHeadingBounds as DOMRect,
            relativeDescBounds as DOMRect,
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
          id: `footer-dot-${row}-${col}`,
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
      setIsResizing(true);
      if (resizeTimeoutRef.current) clearTimeout(resizeTimeoutRef.current);
      resizeTimeoutRef.current = setTimeout(() => {
        generateDotGrid();
        setTimeout(() => {
          setIsResizing(false);
        }, 50);
      }, 250);
    };

    const observer = new window.IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !isVisible) {
          setIsVisible(true);
        }
      },
      { threshold: 0.1 }
    );

    if (containerRef.current) {
      observer.observe(containerRef.current);
    }

    if (isVisible) {
      document.fonts.ready.then(() => {
        generateDotGrid();
      });
      window.addEventListener("resize", handleResize);
    }

    return () => {
      window.removeEventListener("resize", handleResize);
      if (resizeTimeoutRef.current) clearTimeout(resizeTimeoutRef.current);
      if (containerRef.current) {
        observer.disconnect();
      }
    };
  }, [isVisible]);

  return (
    <div
      ref={containerRef}
      id="footerDotGrid"
      className={`absolute inset-0 overflow-hidden transition-opacity duration-300 ${
        isResizing ? "opacity-0" : "opacity-100"
      }`}
    >
      {isVisible &&
        dots.map((dot) => (
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