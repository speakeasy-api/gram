"use client";

import { useEffect, useRef, useState } from "react";
import {
  motion,
  MotionValue,
  useMotionValue,
  useSpring,
  useInView,
  AnimatePresence,
} from "framer-motion";
import SpeakeasyLogo from "./components/SpeakeasyLogo";
import { Button } from "./components/Button";
import { GridOverlay } from "./components/GridOverlay";
import {
  Zap,
  Key,
  Activity,
  Code2,
  Workflow,
  BookOpen,
  Layers,
  Shuffle,
  Users,
  CheckCircle,
} from "lucide-react";
import { AnimateNumber } from "motion-plus/react";

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

function FooterDotsHeroLike({
  footerHeadingRef,
  footerDescRef,
  footerButtonsRef,
}: {
  footerHeadingRef: React.RefObject<HTMLHeadingElement | null>;
  footerDescRef: React.RefObject<HTMLParagraphElement | null>;
  footerButtonsRef: React.RefObject<HTMLDivElement | null>;
}) {
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

function AnimatedToolCard() {
  const ref = useRef(null);
  const isInView = useInView(ref, {
    once: true,
    amount: 0.5,
  });
  const TOOL_COUNT = 17;
  const [isDeploying, setIsDeploying] = useState(false);
  const [isDeployed, setIsDeployed] = useState(false);

  const handleDeploy = () => {
    if (!isDeploying && !isDeployed) {
      setIsDeploying(true);
      setTimeout(() => {
        setIsDeployed(true);
        setIsDeploying(false);
      }, 1200);
    }
  };

  // Auto-click the button after a delay when in view
  useEffect(() => {
    if (isInView && !isDeployed && !isDeploying) {
      const timer = setTimeout(() => {
        handleDeploy();
      }, 800);
      return () => clearTimeout(timer);
    }
  }, [isInView]);

  return (
    <div ref={ref} className="w-full max-w-sm">
      <div className="relative space-y-4">
        {/* Deploy Button */}
        <motion.div
          className="flex justify-center"
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: isInView ? 1 : 0, y: isInView ? 0 : 20 }}
          transition={{ duration: 0.5, ease: [0.23, 1, 0.32, 1] }}
        >
          <motion.button
            className="relative px-6 py-2.5 rounded-full font-mono text-xs uppercase tracking-wider text-neutral-800 overflow-hidden"
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.98 }}
            animate={{
              boxShadow: isDeploying
                ? "0px 8px 16px rgba(0,0,0,0.1)"
                : isDeployed
                ? "0px 2px 4px rgba(0,0,0,0.05)"
                : "0px 4px 8px rgba(0,0,0,0.05)",
              scale: isDeploying ? 0.95 : 1,
            }}
            onClick={handleDeploy}
            disabled={isDeployed}
          >
            {/* Rainbow border effect */}
            <motion.div
              className="absolute inset-0 p-[1px] rounded-full bg-gradient-primary -z-10"
              animate={{
                opacity: isDeployed ? 0.5 : 1,
              }}
              transition={{ duration: 0.5 }}
            />
            <div className="absolute inset-[1px] rounded-full bg-white -z-10" />

            <span className="relative z-10">
              {isDeployed
                ? "Server Deployed"
                : isDeploying
                ? "Deploying..."
                : "Deploy Server"}
            </span>
          </motion.button>
        </motion.div>

        {/* MCP Server Card */}
        <motion.div
          className="bg-white rounded-xl border border-neutral-200 overflow-hidden"
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{
            opacity: isInView ? 1 : 0,
            scale: isInView ? 1 : 0.95,
          }}
          transition={{ duration: 0.6, delay: 0.2, ease: [0.23, 1, 0.32, 1] }}
          whileHover={{
            boxShadow:
              "0px 16px 32px rgba(0,0,0,0.1), 0px 4px 8px rgba(0,0,0,0.05)",
          }}
        >
          {/* Header */}
          <div className="flex items-center justify-between p-3 sm:p-4 border-b border-neutral-200">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-neutral-100 flex items-center justify-center">
                <span className="text-sm font-display text-neutral-700 relative top-[-1px]">
                  g
                </span>
              </div>
              <h3 className="text-sm font-medium text-neutral-900">
                MCP Server
              </h3>
            </div>
            <motion.div
              className="w-2 h-2 rounded-full relative"
              animate={{
                backgroundColor: isDeployed ? "#10b981" : "#e5e5e5",
              }}
              transition={{ duration: 0.3 }}
            >
              {isDeployed && (
                <motion.div
                  className="absolute inset-0 w-2 h-2 bg-success-500 rounded-full"
                  animate={{
                    scale: [1, 2, 2],
                    opacity: [0.8, 0, 0],
                  }}
                  transition={{
                    duration: 2,
                    repeat: Infinity,
                    ease: "easeOut",
                  }}
                />
              )}
            </motion.div>
          </div>

          {/* Content */}
          <div className="p-4 sm:p-6">
            <div className="flex items-center justify-center min-h-[120px]">
              <AnimatePresence mode="wait">
                {!isDeployed ? (
                  <motion.div
                    key="waiting"
                    className="text-center"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.3 }}
                  >
                    <div className="text-sm text-neutral-500">
                      {isDeploying
                        ? "Deploying your MCP server..."
                        : "Ready to deploy"}
                    </div>
                  </motion.div>
                ) : (
                  <motion.div
                    key="deployed"
                    className="text-center space-y-4"
                    initial={{ opacity: 0, scale: 1.1 }}
                    animate={{ opacity: 1, scale: 1 }}
                    transition={{ duration: 0.3 }}
                  >
                    <div>
                      <AnimateNumber
                        className="text-4xl font-mono text-neutral-900 tabular-nums"
                        transition={{
                          visualDuration: 1.2,
                          type: "spring",
                          bounce: 0.25,
                        }}
                      >
                        {isDeployed ? TOOL_COUNT : 0}
                      </AnimateNumber>
                      <div className="text-xs text-neutral-500 mt-1">
                        tools available
                      </div>
                    </div>

                    <div className="flex items-center justify-center gap-4 text-xs">
                      <div className="flex items-center gap-1">
                        <div className="w-1 h-1 rounded-full bg-success-500" />
                        <span className="text-neutral-600">
                          <span className="font-mono text-success-700">
                            12ms
                          </span>{" "}
                          avg
                        </span>
                      </div>
                      <div className="flex items-center gap-1">
                        <div className="w-1 h-1 rounded-full bg-brand-blue-500" />
                        <span className="text-neutral-600">
                          <span className="font-mono text-brand-blue-700">
                            99.9%
                          </span>{" "}
                          uptime
                        </span>
                      </div>
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>
        </motion.div>
      </div>
    </div>
  );
}

function StackedMetricCards() {
  const ref = useRef(null);
  const isInView = useInView(ref, {
    once: true,
    amount: 0.5,
  });
  const [hoveredCard, setHoveredCard] = useState<number | null>(null);

  const cards = [
    {
      id: 1,
      title: "Requests/hour",
      value: "18.2k",
      change: "↑ 47%",
      changeColor: "text-success-600",
      position: { x: "0%", y: "0%" },
      rotate: -2,
      chart: (
        <div className="h-8 flex items-end gap-0.5 w-full">
          {[40, 45, 52, 48, 65, 72, 88, 100].map((height, i) => (
            <div
              key={i}
              className={`flex-1 rounded-sm min-w-0 ${
                i >= 6 ? "bg-success-500" : "bg-neutral-200"
              }`}
              style={{ height: `${height}%` }}
            />
          ))}
        </div>
      ),
    },
    {
      id: 2,
      title: "Response time",
      value: "12ms",
      change: "p99",
      changeColor: "text-info-600",
      position: { x: "45%", y: "15%" },
      rotate: 1.5,
      chart: (
        <div className="space-y-1.5 w-full">
          <div className="flex items-center gap-1 sm:gap-2">
            <span className="text-[10px] text-neutral-500 flex-shrink-0">
              p50
            </span>
            <div className="flex-1 h-1.5 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-400 rounded-full"
                style={{ width: "35%" }}
              />
            </div>
            <span className="text-[10px] text-neutral-700 font-mono flex-shrink-0">
              8ms
            </span>
          </div>
          <div className="flex items-center gap-1 sm:gap-2">
            <span className="text-[10px] text-neutral-500 flex-shrink-0">
              p99
            </span>
            <div className="flex-1 h-1.5 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-500 rounded-full"
                style={{ width: "60%" }}
              />
            </div>
            <span className="text-[10px] text-neutral-700 font-mono flex-shrink-0">
              12ms
            </span>
          </div>
        </div>
      ),
    },
    {
      id: 3,
      title: "Uptime",
      value: "99.9%",
      change: "SLA",
      changeColor: "text-success-600",
      position: { x: "20%", y: "50%" },
      rotate: -1,
      chart: (
        <>
          <div className="grid grid-cols-7 gap-0.5 w-full">
            {Array.from({ length: 28 }, (_, i) => (
              <div
                key={i}
                className={`aspect-square rounded-sm ${
                  i === 14 ? "bg-warning-400" : "bg-success-400"
                }`}
              />
            ))}
          </div>
          <p className="text-[9px] text-neutral-500 mt-1.5">Last 28 days</p>
        </>
      ),
    },
  ];

  return (
    <div ref={ref} className="relative w-full h-[380px] mx-auto">
      {cards.map((card, index) => {
        const isHovered = hoveredCard === card.id;
        const anyHovered = hoveredCard !== null;

        return (
          <motion.div
            key={card.id}
            className="absolute w-[55%] max-w-[220px] min-w-[180px]"
            initial={{
              left: card.position.x,
              top: card.position.y,
              scale: 0.8,
              opacity: 0,
              rotate: card.rotate,
            }}
            animate={{
              left: card.position.x,
              top: card.position.y,
              scale: isInView
                ? isHovered
                  ? 1.05
                  : anyHovered
                  ? 0.95
                  : 1
                : 0.8,
              opacity: isInView ? (anyHovered && !isHovered ? 0.6 : 1) : 0,
              filter: anyHovered && !isHovered ? "blur(2px)" : "blur(0px)",
              rotate: isInView ? (isHovered ? 0 : card.rotate) : card.rotate,
            }}
            transition={{
              scale: {
                type: "spring",
                stiffness: 300,
                damping: 25,
              },
              opacity: {
                duration: 0.3,
              },
              filter: {
                duration: 0.3,
              },
              rotate: {
                type: "spring",
                stiffness: 200,
                damping: 20,
              },
              left: {
                type: "spring",
                stiffness: 260,
                damping: 30,
                delay: isInView ? index * 0.1 : 0,
              },
              top: {
                type: "spring",
                stiffness: 260,
                damping: 30,
                delay: isInView ? index * 0.1 : 0,
              },
            }}
            style={{
              zIndex: isHovered ? 10 : 3 - index,
              transformOrigin: "center center",
            }}
            onMouseEnter={() => setHoveredCard(card.id)}
            onMouseLeave={() => setHoveredCard(null)}
          >
            <motion.div
              className="bg-background-pure rounded-xl border border-neutral-200 p-3 sm:p-4 md:p-5 cursor-pointer"
              animate={{
                boxShadow: isHovered
                  ? "0 20px 40px -8px rgba(0,0,0,0.15), 0 8px 16px -4px rgba(0,0,0,0.08)"
                  : "0 4px 24px -4px rgba(0,0,0,0.08)",
              }}
              transition={{ duration: 0.3 }}
            >
              <div className="flex flex-col h-full">
                <div className="mb-3">
                  <p className="text-[11px] sm:text-xs text-neutral-600 mb-1 truncate">
                    {card.title}
                  </p>
                  <div className="flex items-baseline gap-2">
                    <p className="text-xl sm:text-2xl font-mono text-neutral-900 font-light">
                      {card.value}
                    </p>
                    <span
                      className={`text-[10px] sm:text-xs ${card.changeColor} flex-shrink-0`}
                    >
                      {card.change}
                    </span>
                  </div>
                </div>
                <div className="flex-1 flex flex-col justify-end min-h-[60px]">
                  {card.chart}
                </div>
              </div>
            </motion.div>
          </motion.div>
        );
      })}
    </div>
  );
}

function CurateToolsetsAnimation() {
  const ref = useRef(null);
  const isInView = useInView(ref, { once: true, amount: 0.5 });

  const toolsets = [
    {
      id: "engineering",
      name: "Engineering",
      icon: "E",
      color: "neutral",
      tools: [
        { name: "your-api", type: "internal", label: "/deploy" },
        { name: "github", type: "external", label: "GitHub" },
        { name: "datadog", type: "external", label: "Datadog" },
        { name: "your-api-2", type: "internal", label: "/logs" },
      ],
    },
    {
      id: "sales",
      name: "Sales",
      icon: "S",
      color: "neutral",
      tools: [
        { name: "your-api-3", type: "internal", label: "/customers" },
        { name: "salesforce", type: "external", label: "Salesforce" },
        { name: "hubspot", type: "external", label: "HubSpot" },
        { name: "your-api-4", type: "internal", label: "/analytics" },
      ],
    },
    {
      id: "marketing",
      name: "Marketing",
      icon: "M",
      color: "neutral",
      tools: [
        { name: "your-api-5", type: "internal", label: "/campaigns" },
        { name: "mailchimp", type: "external", label: "Mailchimp" },
        { name: "analytics", type: "external", label: "Analytics" },
        { name: "your-api-6", type: "internal", label: "/content" },
      ],
    },
  ];

  return (
    <div ref={ref} className="w-full max-w-sm">
      <div className="space-y-2">
        {toolsets.map((toolset, index) => (
          <motion.div
            key={toolset.id}
            className="bg-white rounded-xl p-4 border border-neutral-200"
            initial={{ opacity: 0, y: 20 }}
            animate={{
              opacity: isInView ? 1 : 0,
              y: isInView ? 0 : 20,
            }}
            transition={{
              duration: 0.5,
              delay: index * 0.1,
              ease: [0.21, 0.47, 0.32, 0.98],
            }}
          >
            <div className="flex items-center gap-3 mb-3">
              <div className="w-8 h-8 rounded-lg bg-neutral-100 flex items-center justify-center">
                <span className="text-xs font-bold text-neutral-700">
                  {toolset.icon}
                </span>
              </div>
              <div>
                <div className="text-sm font-medium text-neutral-900">
                  {toolset.name}
                </div>
                <div className="text-xs text-neutral-500">
                  {toolset.tools.filter((t) => t.type === "internal").length}{" "}
                  internal,{" "}
                  {toolset.tools.filter((t) => t.type === "external").length}{" "}
                  external
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-2">
              {toolset.tools.map((tool, toolIndex) => (
                <motion.div
                  key={tool.name}
                  className={`px-2.5 py-1 rounded-md text-xs font-medium ${
                    tool.type === "internal"
                      ? "bg-neutral-100 text-neutral-900 ring-1 ring-inset ring-neutral-900/10"
                      : "bg-neutral-50 text-neutral-600"
                  }`}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: isInView ? 1 : 0 }}
                  transition={{
                    delay: index * 0.1 + 0.2 + toolIndex * 0.05,
                    duration: 0.3,
                  }}
                >
                  {tool.label}
                </motion.div>
              ))}
            </div>
          </motion.div>
        ))}
      </div>
    </div>
  );
}

function GramEcosystemAnimation() {
  const ref = useRef(null);
  const isInView = useInView(ref, { once: true, amount: 0.5 });

  return (
    <div ref={ref} className="w-full max-w-2xl mx-auto">
      <motion.div
        className="relative h-[200px] flex items-center justify-center"
        initial={{ opacity: 0 }}
        animate={{ opacity: isInView ? 1 : 0 }}
        transition={{ duration: 0.8, ease: [0.21, 0.47, 0.32, 0.98] }}
      >
        {/* Left: AI Agents */}
        <motion.div
          className="absolute left-0"
          initial={{ opacity: 0, scale: 0.8 }}
          animate={{ opacity: isInView ? 1 : 0, scale: isInView ? 1 : 0.8 }}
          transition={{
            delay: 0,
            duration: 0.5,
            ease: [0.21, 0.47, 0.32, 0.98],
          }}
        >
          <div className="text-center">
            <div className="w-16 h-16 rounded-xl bg-neutral-900 flex items-center justify-center mx-auto mb-3">
              <Users className="w-8 h-8 text-white" />
            </div>
            <div className="font-medium text-sm text-neutral-400">
              AI Agents
            </div>
          </div>
        </motion.div>

        {/* Right: APIs */}
        <motion.div
          className="absolute right-0"
          initial={{ opacity: 0, scale: 0.8 }}
          animate={{ opacity: isInView ? 1 : 0, scale: isInView ? 1 : 0.8 }}
          transition={{
            delay: 0.1,
            duration: 0.5,
            ease: [0.21, 0.47, 0.32, 0.98],
          }}
        >
          <div className="text-center">
            <div className="w-16 h-16 rounded-xl bg-neutral-900 flex items-center justify-center mx-auto mb-3">
              <Layers className="w-8 h-8 text-white" />
            </div>
            <div className="font-medium text-sm text-neutral-400">
              Your APIs
            </div>
          </div>
        </motion.div>

        {/* Center: Gram */}
        <motion.div
          className="relative z-10"
          initial={{ scale: 0, opacity: 0 }}
          animate={{ scale: isInView ? 1 : 0, opacity: isInView ? 1 : 0 }}
          transition={{
            delay: 0.3,
            duration: 0.6,
            type: "spring",
            stiffness: 260,
            damping: 20,
          }}
        >
          <div className="w-24 h-24 rounded-2xl relative">
            <div className="absolute inset-0 rounded-2xl bg-gradient-primary" />
            <div className="absolute inset-[3px] rounded-[13px] bg-white flex items-center justify-center shadow-lg">
              <span className="text-4xl font-display font-light text-neutral-900 relative top-[-4px]">
                g
              </span>
            </div>
          </div>
        </motion.div>

        {/* Animated connection lines */}
        <div className="absolute inset-0 flex items-center justify-center">
          {/* Left line */}
          <motion.div
            className="absolute h-[2px] bg-gradient-to-r from-transparent via-neutral-400 to-neutral-400"
            style={{
              left: "80px",
              right: "50%",
              marginRight: "60px",
              transformOrigin: "left center",
            }}
            initial={{ scaleX: 0, opacity: 0 }}
            animate={{ scaleX: isInView ? 1 : 0, opacity: isInView ? 0.6 : 0 }}
            transition={{ delay: 0.6, duration: 0.4, ease: "easeOut" }}
          />

          {/* Right line */}
          <motion.div
            className="absolute h-[2px] bg-gradient-to-r from-neutral-400 via-neutral-400 to-transparent"
            style={{
              left: "50%",
              marginLeft: "60px",
              right: "80px",
              transformOrigin: "left center",
            }}
            initial={{ scaleX: 0, opacity: 0 }}
            animate={{ scaleX: isInView ? 1 : 0, opacity: isInView ? 0.6 : 0 }}
            transition={{ delay: 0.7, duration: 0.4, ease: "easeOut" }}
          />
        </div>
      </motion.div>
    </div>
  );
}

function APIToolsSection() {
  const [hoveredFeature, setHoveredFeature] = useState<number>(-1);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0 min-h-[400px] md:min-h-[500px]">
      <div className="flex flex-col justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
        <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4 sm:mb-6 max-w-3xl">
          Transform APIs into powerful AI tools
        </h2>
        <p className="text-base sm:text-lg text-neutral-600 mb-6 sm:mb-8">
          Transform your OpenAPI specs into powerful, well-documented AI tools.
        </p>
        <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
          <li
            className="flex items-start gap-3"
            onMouseEnter={() => setHoveredFeature(0)}
            onMouseLeave={() => setHoveredFeature(-1)}
          >
            <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
              <Code2 className="w-4 h-4 text-neutral-900" />
            </div>
            <span>
              <span className="underline decoration-dotted underline-offset-2 hover:text-neutral-900 transition-colors">
                Autogenerate tool definitions
              </span>{" "}
              from OpenAPI
            </span>
          </li>
          <li
            className="flex items-start gap-3"
            onMouseEnter={() => setHoveredFeature(1)}
            onMouseLeave={() => setHoveredFeature(-1)}
          >
            <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
              <Workflow className="w-4 h-4 text-neutral-900" />
            </div>
            <span>
              Craft{" "}
              <span className="underline decoration-dotted underline-offset-2 hover:text-neutral-900 transition-colors">
                higher order tools
              </span>{" "}
              for complex agentic workflows
            </span>
          </li>
          <li className="flex items-start gap-3">
            <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
              <BookOpen className="w-4 h-4 text-neutral-900" />
            </div>
            <span>
              Catalog and distribute prompt templates to make your tools useful
              for everyone
            </span>
          </li>
        </ul>
      </div>
      <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
        <AnimatedAPITransform activeFeature={hoveredFeature} />
      </div>
    </div>
  );
}

function AnimatedAPITransform({ activeFeature }: { activeFeature: number }) {
  const ref = useRef(null);
  const isInView = useInView(ref, { once: true, amount: 0.5 });
  const [hasTransformed, setHasTransformed] = useState(false);
  const [hoveredCard, setHoveredCard] = useState<
    "spec" | "tools" | "higher" | null
  >(null);

  useEffect(() => {
    if (isInView && !hasTransformed) {
      setTimeout(() => {
        setHasTransformed(true);
      }, 600);
    }
  }, [isInView, hasTransformed]);

  const showHigherOrder = hasTransformed && activeFeature === 1;

  return (
    <div ref={ref} className="w-full max-w-lg">
      <div className="relative h-[280px] sm:h-[320px] md:h-[340px]">
        {/* Background: OpenAPI Spec */}
        <motion.div
          className={`absolute left-[12.5%] top-[10%] w-[75%] bg-gradient-to-br from-neutral-100 to-neutral-50 rounded-xl overflow-hidden border border-neutral-200 ${
            hasTransformed ? "cursor-pointer" : ""
          }`}
          onMouseEnter={() => hasTransformed && setHoveredCard("spec")}
          onMouseLeave={() => setHoveredCard(null)}
          style={{
            zIndex: hoveredCard === "spec" ? 20 : 1,
            boxShadow:
              hoveredCard === "spec"
                ? "0px 20px 40px rgba(0,0,0,0.15), 0px 8px 16px rgba(0,0,0,0.1)"
                : "0px 2px 4px rgba(0,0,0,0.05)",
          }}
          animate={{
            scale: !hasTransformed
              ? 1
              : hoveredCard === "spec"
              ? 1.02
              : showHigherOrder
              ? 0.96
              : 0.98,
            filter: !hasTransformed
              ? "blur(0px)"
              : hoveredCard === "spec"
              ? "blur(0px)"
              : hoveredCard === "tools" ||
                hoveredCard === "higher" ||
                showHigherOrder
              ? "blur(2.5px)"
              : "blur(1.5px)",
            opacity: !hasTransformed
              ? 1
              : hoveredCard === "spec"
              ? 1
              : hoveredCard === "tools" ||
                hoveredCard === "higher" ||
                showHigherOrder
              ? 0.7
              : 0.85,
            x: !hasTransformed
              ? 0
              : hoveredCard === "spec"
              ? "-10%"
              : showHigherOrder
              ? "-15%"
              : "-12.5%",
            y: !hasTransformed
              ? 0
              : hoveredCard === "spec"
              ? 20
              : showHigherOrder
              ? 40
              : 30,
            rotate: !hasTransformed
              ? 0
              : hoveredCard === "spec"
              ? 0
              : showHigherOrder
              ? -1.5
              : -1,
          }}
          transition={{
            duration: hoveredCard !== null ? 0.3 : showHigherOrder ? 0.5 : 0.6,
            ease: [0.23, 1, 0.32, 1],
          }}
        >
          <div className="flex items-center gap-2 p-2 sm:p-3 bg-white border-b border-neutral-200">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <rect
                x="2"
                y="3"
                width="12"
                height="10"
                rx="1"
                className="stroke-neutral-400"
                strokeWidth="1.5"
              />
              <path
                d="M5 6.5H11M5 9.5H9"
                className="stroke-neutral-400"
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </svg>
            <span className="text-[10px] sm:text-xs font-medium text-neutral-700">
              PETSTORE.YAML
            </span>
          </div>

          <div className="p-3 sm:p-4 font-mono text-[10px] sm:text-[11px] leading-[1.25] space-y-0.5">
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">1</span>
              <span className="text-brand-green-600">openapi</span>
              <span className="text-neutral-600">: </span>
              <span className="text-brand-blue-600">3.0.0</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">2</span>
              <span className="text-brand-green-600">paths</span>
              <span className="text-neutral-600">:</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">3</span>
              <span className="ml-2"></span>
              <span className="text-brand-green-600">/pet/:id</span>
              <span className="text-neutral-600">:</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">4</span>
              <span className="ml-4"></span>
              <span className="text-brand-green-600">get</span>
              <span className="text-neutral-600">: </span>
              <span className="text-brand-blue-600">findPetById</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">5</span>
              <span className="ml-4"></span>
              <span className="text-brand-green-600">delete</span>
              <span className="text-neutral-600">: </span>
              <span className="text-brand-blue-600">deletePet</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">6</span>
              <span className="ml-2"></span>
              <span className="text-brand-green-600">/pet</span>
              <span className="text-neutral-600">:</span>
            </div>
            <div className="flex">
              <span className="text-neutral-400 mr-2 select-none">7</span>
              <span className="ml-4"></span>
              <span className="text-brand-green-600">post</span>
              <span className="text-neutral-600">: </span>
              <span className="text-brand-blue-600">addPet</span>
            </div>
          </div>
        </motion.div>

        {/* Foreground: AI Tools */}
        <motion.div
          className={`absolute right-0 top-8 sm:top-12 w-[78%] ${
            hasTransformed ? "cursor-pointer" : ""
          }`}
          onMouseEnter={() => hasTransformed && setHoveredCard("tools")}
          onMouseLeave={() => setHoveredCard(null)}
          style={{
            zIndex: hoveredCard === "spec" ? 5 : 10,
          }}
          initial={{ opacity: 0, y: 20, scale: 0.95 }}
          animate={{
            opacity: hasTransformed
              ? hoveredCard === "spec"
                ? 0.7
                : hoveredCard === "higher" || showHigherOrder
                ? 0.85
                : 1
              : 0,
            y: hasTransformed ? (showHigherOrder ? 10 : 0) : 20,
            x: showHigherOrder ? -8 : 0,
            scale: hasTransformed
              ? hoveredCard === "tools"
                ? 1.01
                : showHigherOrder
                ? 0.98
                : 1
              : 0.95,
            filter:
              hoveredCard === "spec"
                ? "blur(1px)"
                : hoveredCard === "higher" || showHigherOrder
                ? "blur(1.5px)"
                : "blur(0px)",
          }}
          transition={{
            duration: hoveredCard !== null ? 0.3 : showHigherOrder ? 0.5 : 0.6,
            ease: [0.23, 1, 0.32, 1],
            delay:
              hasTransformed && hoveredCard === null && !showHigherOrder
                ? 0.1
                : 0,
          }}
        >
          <motion.div
            className="w-full bg-white rounded-xl overflow-hidden border border-neutral-200"
            animate={{
              boxShadow:
                hoveredCard === "tools"
                  ? "0px 20px 40px rgba(0,0,0,0.12), 0px 8px 16px rgba(0,0,0,0.08)"
                  : "0px 16px 32px rgba(0,0,0,0.1), 0px 4px 8px rgba(0,0,0,0.05)",
            }}
            transition={{ duration: 0.3, ease: [0.23, 1, 0.32, 1] }}
          >
            <div className="flex items-center justify-between p-2 sm:p-3 border-b border-neutral-200">
              <h4 className="text-[10px] sm:text-xs font-medium text-neutral-900">
                Auto-generated Tools
              </h4>
              <motion.div
                initial={{ scale: 0, rotate: -180 }}
                animate={{ scale: hasTransformed ? 1 : 0, rotate: 0 }}
                transition={{ type: "spring", delay: 0.7 }}
              >
                <CheckCircle className="w-4 h-4 text-success-600" />
              </motion.div>
            </div>

            <div className="p-3 sm:p-4 overflow-hidden">
              <div className="space-y-1.5 overflow-hidden">
                {hasTransformed &&
                  [
                    {
                      name: "findPetById",
                      desc: "GET /pet/{id}",
                      color: "blue",
                    },
                    {
                      name: "deletePet",
                      desc: "DELETE /pet/{id}",
                      color: "red",
                    },
                    { name: "addPet", desc: "POST /pet", color: "green" },
                  ].map((tool, index) => (
                    <motion.div
                      key={tool.name}
                      initial={{ opacity: 0, x: -20 }}
                      animate={{ opacity: 1, x: 0 }}
                      transition={{ delay: 0.5 + index * 0.08, duration: 0.3 }}
                      className="flex items-center gap-3 p-2 rounded-md"
                    >
                      <div
                        className={`w-1.5 h-1.5 rounded-full ${
                          tool.color === "blue"
                            ? "bg-brand-blue-500"
                            : tool.color === "green"
                            ? "bg-brand-green-500"
                            : tool.color === "yellow"
                            ? "bg-warning-500"
                            : tool.color === "red"
                            ? "bg-brand-red-500"
                            : ""
                        }`}
                      />
                      <div className="flex-1">
                        <div className="font-mono text-[10px] sm:text-[11px] text-neutral-900">
                          {tool.name}
                        </div>
                        <div className="text-[8px] sm:text-[9px] text-neutral-500">
                          {tool.desc}
                        </div>
                      </div>
                    </motion.div>
                  ))}
              </div>
            </div>
          </motion.div>
        </motion.div>

        {/* Third Layer: Higher Order Tools */}
        <motion.div
          className={`absolute right-0 top-8 sm:top-12 w-[72%] ${
            showHigherOrder ? "cursor-pointer" : ""
          }`}
          onMouseEnter={() => showHigherOrder && setHoveredCard("higher")}
          onMouseLeave={() => setHoveredCard(null)}
          style={{
            zIndex: showHigherOrder ? 30 : 0,
          }}
          initial={{ opacity: 0, y: 40, scale: 0.9 }}
          animate={{
            opacity: showHigherOrder ? 1 : 0,
            y: showHigherOrder ? 0 : 40,
            scale: showHigherOrder
              ? hoveredCard === "higher"
                ? 1.02
                : 1
              : 0.9,
          }}
          transition={{
            duration: 0.5,
            ease: [0.23, 1, 0.32, 1],
          }}
        >
          <motion.div
            className="w-full bg-white rounded-xl overflow-hidden border border-neutral-200"
            animate={{
              boxShadow:
                hoveredCard === "higher"
                  ? "0px 32px 64px rgba(0,0,0,0.2), 0px 16px 32px rgba(0,0,0,0.15)"
                  : "0px 24px 48px rgba(0,0,0,0.15), 0px 12px 24px rgba(0,0,0,0.1)",
            }}
            transition={{ duration: 0.3, ease: [0.23, 1, 0.32, 1] }}
          >
            <div className="flex items-center justify-between p-2 sm:p-3 border-b border-neutral-200">
              <h4 className="text-[10px] sm:text-xs font-medium text-neutral-900">
                Higher Order Tool
              </h4>
              <motion.div
                initial={{ scale: 0, rotate: -180 }}
                animate={{ scale: 1, rotate: 0 }}
                transition={{ type: "spring", delay: 0.3 }}
              >
                <Workflow className="w-4 h-4 text-brand-blue-600" />
              </motion.div>
            </div>

            <div className="p-3 sm:p-4 overflow-hidden">
              <div className="space-y-3">
                <motion.div
                  initial={{ opacity: 0, x: -20 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ delay: 0.4, duration: 0.3 }}
                  className="flex items-center gap-3"
                >
                  <div className="w-2 h-2 rounded-full bg-brand-blue-500" />
                  <div className="flex-1">
                    <div className="font-mono text-[10px] sm:text-[11px] text-neutral-900">
                      registerNewPet
                    </div>
                    <div className="text-[8px] sm:text-[9px] text-neutral-500">
                      Validates and registers a new pet in one workflow
                    </div>
                  </div>
                </motion.div>

                <motion.div
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: 0.5, duration: 0.3 }}
                  className="ml-5 space-y-1 text-[8px] sm:text-[9px] text-neutral-500 font-mono"
                >
                  <div className="flex items-center gap-2">
                    <span className="text-neutral-400">1.</span>
                    <span>Check if exists</span>
                    <span className="text-neutral-400">→</span>
                    <span className="text-brand-blue-600">findPetById</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-neutral-400">2.</span>
                    <span>Create record</span>
                    <span className="text-neutral-400">→</span>
                    <span className="text-brand-green-600">addPet</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-neutral-400">3.</span>
                    <span>Set status</span>
                    <span className="text-neutral-400">→</span>
                    <span className="text-warning-600">updatePet</span>
                  </div>
                </motion.div>
              </div>
            </div>
          </motion.div>
        </motion.div>
      </div>
    </div>
  );
}

export default function Home() {
  const [dots, setDots] = useState<Dot[]>([]);
  const [isResizing, setIsResizing] = useState(false);
  const [active, setActive] = useState({ row: 0, col: 0 });
  const resizeTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const [showNavbarCTA, setShowNavbarCTA] = useState(false);
  const [isMobile, setIsMobile] = useState(false);

  const dragX = useMotionValue(0);
  const dragY = useMotionValue(0);

  const introducingRef = useRef<HTMLHeadingElement>(null);
  const gramRef = useRef<HTMLHeadingElement>(null);
  const descriptionRef = useRef<HTMLDivElement>(null);
  const buttonsRef = useRef<HTMLDivElement>(null);

  const footerHeadingRef = useRef<HTMLHeadingElement>(null);
  const footerDescRef = useRef<HTMLParagraphElement>(null);
  const footerButtonsRef = useRef<HTMLDivElement>(null);

  const footerRef = useRef<HTMLDivElement>(null);
  const isFooterInView = useInView(footerRef, { amount: 0.1 });

  const shouldSkipDot = (
    x: number,
    y: number,
    introducingBounds: DOMRect,
    gramBounds: DOMRect,
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

  const generateDotGrid = () => {
    const container = document.getElementById("dotGrid");
    if (!container) return;

    const isMobile = window.innerWidth < 768;
    const isTablet = window.innerWidth >= 768 && window.innerWidth < 1024;

    const containerBounds = container.getBoundingClientRect();
    const introducingBounds = introducingRef.current?.getBoundingClientRect();
    const gramBounds = gramRef.current?.getBoundingClientRect();
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
      setIsResizing(true);

      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current);
      }

      resizeTimeoutRef.current = setTimeout(() => {
        generateDotGrid();
        setTimeout(() => {
          setIsResizing(false);
        }, 50);
      }, 250);
    };

    document.fonts.ready.then(() => {
      generateDotGrid();
    });

    window.addEventListener("resize", handleResize);

    const heroObserver = new window.IntersectionObserver(
      ([entry]) => {
        setShowNavbarCTA(!entry.isIntersecting);
      },
      {
        threshold: 0,
        rootMargin: "-80px 0px 0px 0px",
      }
    );

    if (buttonsRef.current) {
      heroObserver.observe(buttonsRef.current);
    }

    const checkMobile = () => {
      setIsMobile(window.innerWidth < 768);
    };
    checkMobile();
    window.addEventListener("resize", checkMobile);
    return () => {
      window.removeEventListener("resize", handleResize);
      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current);
      }
      if (buttonsRef.current) {
        heroObserver.unobserve(buttonsRef.current);
      }
      heroObserver.disconnect();
    };
  }, []);

  return (
    <>
      <header className="header-base">
        <div className="absolute top-0 left-0 right-0 h-1 w-full bg-gradient-primary" />
        <div className="flex justify-between items-center px-6 md:px-10 lg:px-16 pt-1 w-full relative overflow-visible">
          <a
            href="https://www.speakeasy.com/"
            className="transition-opacity hover:opacity-80"
          >
            <SpeakeasyLogo className="h-5 w-auto text-foreground" />
          </a>
          <motion.div
            className="flex items-center"
            initial={false}
            animate={{
              justifyContent: showNavbarCTA ? "flex-start" : "flex-end",
              gap: showNavbarCTA ? "16px" : "0px",
            }}
            transition={{
              type: "spring",
              stiffness: 400,
              damping: 40,
              mass: 0.8,
            }}
          >
            <motion.a
              href="https://docs.getgram.ai/"
              className="relative inline-flex items-center justify-center font-mono text-[15px] leading-[1.6] tracking-[0.01em] uppercase whitespace-nowrap rounded-full transition-colors"
              initial={{
                backgroundColor: "rgb(245 245 245)",
                color: "rgb(38 38 38)",
                paddingLeft: "20px",
                paddingRight: "20px",
                paddingTop: "8px",
                paddingBottom: "8px",
                boxShadow:
                  "0px 2px 1px 0px #FFF inset, 0px -2px 1px 100px rgba(0,0,0,0.0) inset, 0px -2px 1px 0px rgba(0,0,0,0.1) inset",
              }}
              animate={{
                backgroundColor: showNavbarCTA
                  ? "transparent"
                  : "rgb(245 245 245)",
                color: showNavbarCTA ? "rgb(64 64 64)" : "rgb(38 38 38)",
                paddingLeft: showNavbarCTA ? "0px" : "20px",
                paddingRight: showNavbarCTA ? "0px" : "20px",
                paddingTop: showNavbarCTA ? "0px" : "8px",
                paddingBottom: showNavbarCTA ? "0px" : "8px",
                boxShadow: showNavbarCTA
                  ? "none"
                  : "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(220,220,220,0.2) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
              }}
              whileHover={{
                color: showNavbarCTA ? "rgb(38 38 38)" : "rgb(38 38 38)",
                backgroundColor: showNavbarCTA
                  ? "transparent"
                  : "rgb(245 245 245)",
                boxShadow: showNavbarCTA
                  ? "none"
                  : "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(220,220,220,0.2) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
              }}
              transition={{
                type: "spring",
                stiffness: 500,
                damping: 40,
                mass: 0.5,
              }}
            >
              View docs
            </motion.a>
            {!isMobile && (
              <motion.div
                initial={{
                  width: 0,
                  opacity: 0,
                }}
                animate={{
                  width: showNavbarCTA ? "auto" : 0,
                  opacity: showNavbarCTA ? 1 : 0,
                }}
                transition={{
                  width: {
                    type: "spring",
                    stiffness: 400,
                    damping: 40,
                    mass: 0.8,
                  },
                  opacity: {
                    duration: 0.2,
                    ease: "easeOut",
                  },
                }}
                style={{
                  overflow: "hidden",
                  display: "flex",
                }}
              >
                <div className="relative rounded-full overflow-hidden">
                  <Button
                    variant="rainbow-light"
                    href="https://speakeasyapi.typeform.com/to/h6WJdwWr"
                    className="shadow-lg whitespace-nowrap"
                  >
                    Join the waitlist
                  </Button>
                </div>
              </motion.div>
            )}
          </motion.div>
        </div>
      </header>

      <div className="relative min-h-screen">
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

        <div className="relative z-20 pointer-events-none">
          <h1
            ref={introducingRef}
            className="absolute top-[20vh] md:top-[140px] left-6 md:left-10 lg:left-40 font-display font-light text-display-sm md:text-display-md lg:text-display-lg leading-[0.8] tracking-tight"
          >
            Introducing
          </h1>

          <h2
            ref={gramRef}
            className="absolute top-[45vh] left-1/2 md:left-1/2 lg:left-auto lg:right-60 -translate-x-[20%] md:-translate-x-[10%] lg:translate-x-0 font-display font-light text-[5rem] md:text-[8rem] lg:text-[11.25rem] leading-[0.7] tracking-tighter"
          >
            gram.
          </h2>
        </div>

        <div className="absolute bottom-8 left-6 right-6 md:bottom-10 md:right-10 lg:bottom-24 lg:right-24 md:left-auto z-30">
          <div className="flex flex-col gap-6 lg:gap-8 items-center md:items-start">
            <div ref={descriptionRef} className="max-w-md">
              <p className="text-foreground/80 text-sm md:text-base lg:text-[1.0625rem] leading-relaxed text-center md:text-left">
                Create, curate and distribute tools for AI
                <br />
                Everything you need to power
                <br />
                integrations for Agents and LLMs
              </p>
            </div>

            <div
              ref={buttonsRef}
              className="flex flex-col md:flex-row gap-3 w-full md:w-auto"
            >
              <Button
                size="chunky"
                variant="rainbow-light"
                href="https://speakeasyapi.typeform.com/to/h6WJdwWr"
              >
                Join the waitlist
              </Button>
              <Button
                size="chunky"
                variant="primary-dark"
                href="https://calendly.com/sagar-speakeasy/30min"
              >
                Book a demo
              </Button>
            </div>
          </div>
        </div>
      </div>

      <section className="w-full py-24 sm:py-32 lg:py-40 relative">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8 relative">
          {/* Center divider line - moved outside to span full section */}
          <div className="absolute left-1/2 top-0 bottom-0 w-px bg-neutral-200 -translate-x-1/2 hidden md:block" />

          {/* Decoration squares where center line meets container top/bottom */}
          <div
            className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-20"
            style={{
              width: 10,
              height: 10,
              background: "var(--color-background)",
              border: "2px solid var(--color-neutral-200)",
              borderRadius: 2,
            }}
          />
          <div
            className="absolute left-1/2 bottom-0 -translate-x-1/2 translate-y-1/2 hidden md:block z-20"
            style={{
              width: 10,
              height: 10,
              background: "var(--color-background)",
              border: "2px solid var(--color-neutral-200)",
              borderRadius: 2,
            }}
          />

          <div className="relative space-y-24 sm:space-y-32 lg:space-y-40">
            <GridOverlay inset="0" />

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0 min-h-[400px] md:min-h-[500px]">
              <div className="flex flex-col justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4 sm:mb-6">
                  Easiest way to host MCP at scale
                </h2>
                <p className="text-base sm:text-lg text-neutral-600 mb-6 sm:mb-8">
                  High quality Agentic Tools. Enterprise Experience.
                </p>
                <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Zap className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>1-click hosting of Toolsets as MCP servers</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Key className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Support for managed and passthrough API authentication
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Built in telemetry, audit logs</span>
                  </li>
                </ul>
              </div>
              <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <AnimatedToolCard />
              </div>
            </div>

            {/* Section divider */}
            <div className="relative h-0">
              {/* Left horizontal line - extends to center + 15px */}
              <div
                className="absolute left-0 top-0 h-px hidden md:block"
                style={{
                  right: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Mobile version - single line */}
              <div className="absolute inset-x-0 top-0 h-px bg-neutral-200 md:hidden" />

              {/* Decoration squares - left edge, center, right edge */}
              <div
                className="absolute left-0 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
            </div>

            <APIToolsSection />

            {/* Section divider */}
            <div className="relative h-0">
              {/* Left horizontal line - extends to center + 15px */}
              <div
                className="absolute left-0 top-0 h-px hidden md:block"
                style={{
                  right: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Mobile version - single line */}
              <div className="absolute inset-x-0 top-0 h-px bg-neutral-200 md:hidden" />

              {/* Decoration squares - left edge, center, right edge */}
              <div
                className="absolute left-0 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0 min-h-[400px] md:min-h-[500px]">
              <div className="flex flex-col justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4 sm:mb-6 max-w-3xl">
                  Curate Toolsets for every usecase
                </h2>
                <p className="text-base sm:text-lg text-neutral-600 mb-6 sm:mb-8">
                  Organize and optimize your tools for different teams and
                  workflows.
                </p>
                <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Layers className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Easily group tools into Toolsets</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Shuffle className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Remix tools across your APIs and 3P services</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Users className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Scope tool use for teams</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <CheckCircle className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Instantly test and run evals for quality</span>
                  </li>
                </ul>
              </div>
              <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <CurateToolsetsAnimation />
              </div>
            </div>

            {/* Section divider */}
            <div className="relative h-0">
              {/* Left horizontal line - extends to center + 15px */}
              <div
                className="absolute left-0 top-0 h-px hidden md:block"
                style={{
                  right: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-200)",
                }}
              />
              {/* Mobile version - single line */}
              <div className="absolute inset-x-0 top-0 h-px bg-neutral-200 md:hidden" />

              {/* Decoration squares - left edge, center, right edge */}
              <div
                className="absolute left-0 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-200)",
                  borderRadius: 2,
                }}
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0 min-h-[400px] md:min-h-[500px]">
              <div className="flex flex-col justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4 sm:mb-6 max-w-3xl">
                  Enterprise-grade tool distribution
                </h2>
                <p className="text-base sm:text-lg text-neutral-600 mb-6 sm:mb-8">
                  Secure, scalable infrastructure for tool deployment and
                  management.
                </p>
                <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Zap className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>1-click hosting of Toolsets as MCP servers</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Key className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Support for managed and passthrough API authentication
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Built in telemetry, audit logs</span>
                  </li>
                </ul>
              </div>
              <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <StackedMetricCards />
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Pre-footer CTA section */}
      <section className="w-full py-28 sm:py-36 lg:py-44 bg-black relative overflow-hidden">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex flex-col items-center text-center">
            <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-8 sm:mb-12 max-w-4xl mx-auto text-white">
              Build AI that works. Unlock API and Data for Agents. Secure and
              Composable.
            </h2>
            <GramEcosystemAnimation />
          </div>
        </div>
      </section>

      <footer
        ref={footerRef}
        className="relative bg-neutral-100 w-full border-t border-neutral-200 overflow-hidden min-h-[600px] flex flex-col justify-center items-center"
      >
        <FooterDotsHeroLike
          footerHeadingRef={footerHeadingRef}
          footerDescRef={footerDescRef}
          footerButtonsRef={footerButtonsRef}
        />
        <div className="relative z-20 w-full pointer-events-none">
          <div className="flex flex-col items-center justify-center py-32 sm:py-40 lg:py-48 max-w-2xl mx-auto px-4">
            <h3
              ref={footerHeadingRef}
              className="text-4xl md:text-5xl lg:text-6xl font-display font-light text-neutral-900 mb-8 sm:mb-10 text-center max-w-3xl pointer-events-auto"
            >
              Ready to create, curate, and distribute tools for AI?
            </h3>
            <p
              ref={footerDescRef}
              className="text-lg sm:text-xl text-neutral-700 mb-10 sm:mb-12 text-center max-w-xl pointer-events-auto leading-relaxed"
            >
              Power your integrations for Agents and LLMs. Join the waitlist or
              book a demo to get started.
            </p>
            <div
              ref={footerButtonsRef}
              className="flex flex-col md:flex-row gap-4 w-full md:w-auto justify-center pointer-events-auto"
            >
              <Button
                size="chunky"
                variant="rainbow-light"
                href="https://speakeasyapi.typeform.com/to/h6WJdwWr"
              >
                Join the waitlist
              </Button>
              <Button
                size="chunky"
                variant="primary-dark"
                href="https://calendly.com/sagar-speakeasy/30min"
              >
                Book a demo
              </Button>
            </div>
          </div>
        </div>

        <div className="absolute left-0 right-0 bottom-0 h-1 w-full bg-gradient-primary z-20" />
      </footer>
      <AnimatePresence>
        {showNavbarCTA && isMobile && !isFooterInView && (
          <motion.div
            initial={{ y: 100, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 100, opacity: 0 }}
            transition={{ type: "spring", stiffness: 400, damping: 30 }}
            className="fixed bottom-4 left-4 right-4 z-[1000] flex justify-center pointer-events-auto"
          >
            <Button
              variant="rainbow-light"
              href="https://speakeasyapi.typeform.com/to/h6WJdwWr"
              className="w-full max-w-xs shadow-lg text-base py-4"
            >
              Join the waitlist
            </Button>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}
