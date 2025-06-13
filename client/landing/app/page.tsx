"use client";

import { useEffect, useRef, useState } from "react";
import {
  motion,
  MotionValue,
  useMotionValue,
  useSpring,
  useInView,
} from "framer-motion";
import SpeakeasyLogo from "./components/SpeakeasyLogo";
import { Button } from "./components/Button";
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
      onDragEnd={() => {
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

export default function Home() {
  const [dots, setDots] = useState<Dot[]>([]);
  const [isResizing, setIsResizing] = useState(false);
  const [active, setActive] = useState({ row: 0, col: 0 });
  const resizeTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const [showNavbarCTA, setShowNavbarCTA] = useState(false);

  const dragX = useMotionValue(0);
  const dragY = useMotionValue(0);

  const introducingRef = useRef<HTMLHeadingElement>(null);
  const gramRef = useRef<HTMLHeadingElement>(null);
  const descriptionRef = useRef<HTMLDivElement>(null);
  const buttonsRef = useRef<HTMLDivElement>(null);

  const footerHeadingRef = useRef<HTMLHeadingElement>(null);
  const footerDescRef = useRef<HTMLParagraphElement>(null);
  const footerButtonsRef = useRef<HTMLDivElement>(null);

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

      <section className="w-full max-w-6xl mx-auto px-2 sm:px-4 py-16 sm:py-24 space-y-16 sm:space-y-24">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0">
          <div className="flex flex-col justify-center px-2 sm:px-6 py-8 sm:py-12">
            <h2 className="font-display text-2xl sm:text-3xl md:text-4xl lg:text-display-lg mb-3 sm:mb-4">
              Easiest way to host MCP at scale
            </h2>
            <p className="text-base sm:text-lg md:text-xl text-foreground/80 mb-4 sm:mb-6">
              High quality Agentic Tools. Enterprise Experience
            </p>
            <ul className="space-y-2 sm:space-y-3 text-sm sm:text-base text-foreground/60">
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Zap className="w-4 h-4 text-black" />
                </div>
                <span>1-click hosting of Toolsets as MCP servers</span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Key className="w-4 h-4 text-black" />
                </div>
                <span>
                  Support for managed and passthrough API authentication
                </span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Activity className="w-4 h-4 text-black" />
                </div>
                <span>Built in telemetry, audit logs</span>
              </li>
            </ul>
          </div>
          <div className="border-l border-[var(--color-neutral-200)] flex items-center justify-center px-2 sm:px-8 py-8 sm:py-12 bg-gradient-to-br from-[var(--color-neutral-100)] via-transparent to-transparent">
            <AnimatedToolCard />
          </div>
        </div>

        <div className="border-t" style={{ borderColor: "#dcdcdc" }} />

        <APIToolsSection />
        <div className="border-t" style={{ borderColor: "#dcdcdc" }} />

        <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0">
          <div className="flex flex-col justify-center px-2 sm:px-6 py-8 sm:py-12">
            <h2 className="font-display text-2xl sm:text-3xl md:text-4xl lg:text-display-lg mb-3 sm:mb-4">
              Curate Toolsets for every usecase
            </h2>
            <ul className="space-y-2 sm:space-y-3 text-sm sm:text-base text-foreground/60 mb-4 sm:mb-6">
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Layers className="w-4 h-4 text-black" />
                </div>
                <span>Easily group tools into Toolsets</span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Shuffle className="w-4 h-4 text-black" />
                </div>
                <span>Remix tools across your APIs and 3P services</span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Users className="w-4 h-4 text-black" />
                </div>
                <span>Scope tool use for teams</span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <CheckCircle className="w-4 h-4 text-black" />
                </div>
                <span>Instantly test and run evals for quality</span>
              </li>
            </ul>
          </div>
          <div className="border-l border-border flex items-center justify-center px-2 sm:px-8 py-8 sm:py-12">
            <div className="space-y-2 w-full max-w-xs">
              <div className="bg-background-pure rounded-lg p-3 border-2 border-[var(--color-info-400)] shadow-[0_4px_16px_-4px_rgba(0,0,0,0.08)] flex items-center gap-3">
                <div className="w-8 h-8 rounded-lg bg-[var(--color-info-100)] flex items-center justify-center">
                  <span className="text-xs font-bold text-[var(--color-info-700)]">
                    S
                  </span>
                </div>
                <div className="flex-1">
                  <div className="text-sm font-medium text-[var(--color-neutral-900)]">
                    Slack
                  </div>
                  <div className="text-xs text-[var(--color-neutral-500)]">
                    12 tools
                  </div>
                </div>
              </div>
              <div className="bg-background-pure rounded-lg p-3 border border-[var(--color-neutral-200)] flex items-center gap-3">
                <div className="w-8 h-8 rounded-lg bg-[var(--color-success-100)] flex items-center justify-center">
                  <span className="text-xs font-bold text-[var(--color-success-700)]">
                    Z
                  </span>
                </div>
                <div className="flex-1">
                  <div className="text-sm font-medium text-[var(--color-neutral-900)]">
                    Zendesk
                  </div>
                  <div className="text-xs text-[var(--color-neutral-500)]">
                    8 tools
                  </div>
                </div>
                <svg
                  className="w-4 h-4 text-[var(--color-success-500)]"
                  fill="currentColor"
                  viewBox="0 0 20 20"
                >
                  <path
                    fillRule="evenodd"
                    d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
                    clipRule="evenodd"
                  />
                </svg>
              </div>
              <button className="w-full py-2.5 rounded-lg border border-dashed border-[var(--color-neutral-300)] text-xs text-[var(--color-neutral-600)] hover:border-[var(--color-neutral-400)] transition-colors">
                + Add service
              </button>
            </div>
          </div>
        </div>
        <div className="border-t" style={{ borderColor: "#dcdcdc" }} />

        <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0">
          <div className="border-r border-border flex items-center justify-center px-2 sm:px-8 py-8 sm:py-12 order-2 md:order-1">
            <div className="bg-background-pure rounded-xl shadow-[0_4px_24px_-4px_rgba(0,0,0,0.08)] border border-[var(--color-neutral-200)] p-4 sm:p-5 w-full max-w-xs">
              <div className="mb-3">
                <div className="flex items-baseline gap-2">
                  <p className="text-3xl font-mono text-[var(--color-neutral-900)] font-light">
                    18.2k
                  </p>
                  <span className="text-sm text-[var(--color-success-600)]">
                    ↑ 47%
                  </span>
                </div>
                <p className="text-xs text-[var(--color-neutral-600)]">
                  Requests this hour
                </p>
              </div>
              <div className="h-8 flex items-end gap-0.5">
                {[40, 45, 52, 48, 65, 72, 88, 100].map((height, i) => (
                  <div
                    key={i}
                    className={`flex-1 rounded-sm ${
                      i >= 6
                        ? "bg-[var(--color-success-500)]"
                        : "bg-[var(--color-neutral-200)]"
                    }`}
                    style={{ height: `${height}%` }}
                  />
                ))}
              </div>
            </div>
          </div>
          <div className="flex flex-col justify-center px-2 sm:px-6 py-8 sm:py-12 order-1 md:order-2">
            <h2 className="font-display text-2xl sm:text-3xl md:text-4xl lg:text-display-lg mb-3 sm:mb-4">
              Distribute tools through an Enterprise ready Tools Gateway
            </h2>
            <ul className="space-y-2 sm:space-y-3 text-sm sm:text-base text-foreground/60 mb-4 sm:mb-6">
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Zap className="w-4 h-4 text-black" />
                </div>
                <span>1-click hosting of Toolsets as MCP servers</span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Key className="w-4 h-4 text-black" />
                </div>
                <span>
                  Support for managed and passthrough API authentication
                </span>
              </li>
              <li className="flex items-start gap-2 sm:gap-3">
                <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
                  <Activity className="w-4 h-4 text-black" />
                </div>
                <span>Built in telemetry, audit logs</span>
              </li>
            </ul>
          </div>
        </div>
        <div className="border-t" style={{ borderColor: "#dcdcdc" }} />

        <div className="flex flex-col items-center text-center px-2 sm:px-8 py-8 sm:py-12">
          <h2 className="font-display text-2xl sm:text-3xl md:text-4xl lg:text-display-lg mb-3 sm:mb-4">
            Build AI that works. Unlock API and Data for Agents. Secure and
            Composable.
          </h2>
          <div className="w-full max-w-xs sm:max-w-sm">
            <div className="bg-background-pure rounded-xl shadow-[0_4px_24px_-4px_rgba(0,0,0,0.08)] border border-[var(--color-neutral-200)] p-4 sm:p-6">
              <div className="flex items-center justify-between gap-2 sm:gap-4 mb-4">
                <div className="text-center">
                  <div className="w-10 h-10 rounded-lg bg-[var(--color-neutral-100)] border border-[var(--color-neutral-200)] flex items-center justify-center text-[10px] font-medium text-[var(--color-neutral-700)]">
                    AI
                  </div>
                </div>
                <div className="flex-1 h-[1px] bg-[var(--color-neutral-200)]" />
                <div className="w-10 h-10 rounded-lg relative">
                  <div className="absolute inset-0 rounded-lg bg-gradient-primary" />
                  <div className="absolute inset-[1px] rounded-[9px] bg-background-pure flex items-center justify-center">
                    <span className="text-sm font-display font-light text-[var(--color-neutral-900)]">
                      g
                    </span>
                  </div>
                </div>
                <div className="flex-1 h-[1px] bg-[var(--color-neutral-200)]" />
                <div className="text-center">
                  <div className="w-10 h-10 rounded-lg bg-[var(--color-neutral-900)] flex items-center justify-center text-[10px] font-medium text-white">
                    API
                  </div>
                </div>
              </div>
              <div className="text-center">
                <p className="text-xs text-[var(--color-neutral-600)]">
                  <span className="font-medium text-[var(--color-neutral-900)]">
                    2,847
                  </span>{" "}
                  tools ready
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <footer className="relative bg-white w-full mt-32 border-t border-neutral-200 overflow-hidden min-h-[400px] flex flex-col justify-center items-center">
        <FooterDotsHeroLike
          footerHeadingRef={footerHeadingRef}
          footerDescRef={footerDescRef}
          footerButtonsRef={footerButtonsRef}
        />
        <div className="relative z-10 w-full">
          <div className="flex flex-col items-center justify-center py-20 max-w-2xl mx-auto px-4">
            <h3
              ref={footerHeadingRef}
              className="text-4xl md:text-5xl font-display font-light text-neutral-900 mb-6 text-center max-w-2xl"
            >
              Ready to create, curate, and distribute tools for AI?
            </h3>
            <p
              ref={footerDescRef}
              className="text-lg text-neutral-700 mb-8 text-center max-w-xl"
            >
              Power your integrations for Agents and LLMs. Join the waitlist or
              book a demo to get started.
            </p>
            <div
              ref={footerButtonsRef}
              className="flex flex-col md:flex-row gap-3 w-full md:w-auto justify-center"
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
    </>
  );
}

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
    const startY = 0;
    const endX = containerWidth - paddingX;
    const endY = containerHeight - (isMobile ? 24 : 40);

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
        setTimeout(() => setIsResizing(false), 50);
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
      className={`absolute inset-0 overflow-hidden transition-opacity duration-300 z-0 ${
        isResizing ? "opacity-0" : "opacity-100"
      }`}
      aria-hidden="true"
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
  const isInView = useInView(ref, { once: true, amount: 0.5 });
  const TOOL_COUNT = 17;
  const [isDeployed, setIsDeployed] = useState(false);

  useEffect(() => {
    if (isInView && !isDeployed) {
      setTimeout(() => {
        setIsDeployed(true);
      }, 800);
    }
  }, [isInView, isDeployed]);

  return (
    <div
      ref={ref}
      className="bg-background-pure rounded-2xl shadow-[0_8px_32px_-8px_rgba(0,0,0,0.12)] border border-[var(--color-neutral-200)] p-8 w-full max-w-sm flex flex-col gap-6 relative overflow-hidden"
    >
      <div className="flex items-center gap-4 w-full">
        <motion.div
          className="w-12 h-12 rounded-xl bg-white border border-[var(--color-neutral-200)] flex items-center justify-center shadow-[0_2px_8px_-2px_rgba(0,0,0,0.08)]"
          whileHover={{ scale: 1.05 }}
          transition={{ type: "spring", stiffness: 400, damping: 10 }}
        >
          <span className="text-xl font-display font-light text-[var(--color-neutral-900)] -mt-1">
            g
          </span>
        </motion.div>
        <div className="flex-1 min-w-0">
          <h3 className="truncate text-lg font-medium text-[var(--color-neutral-900)]">
            Your MCP
          </h3>
        </div>
        <motion.div
          className="w-3 h-3 rounded-full relative"
          animate={{
            backgroundColor: isDeployed
              ? "var(--color-success-500)"
              : "var(--color-neutral-300)",
          }}
          transition={{ duration: 0.3 }}
        >
          {isDeployed && (
            <motion.div
              className="absolute inset-0 w-3 h-3 bg-[var(--color-success-400)] rounded-full"
              animate={{
                scale: [1, 1.5, 1.5],
                opacity: [1, 0, 0],
              }}
              transition={{
                duration: 2,
                repeat: Infinity,
                times: [0, 0.5, 1],
                ease: "easeOut",
              }}
            />
          )}
        </motion.div>
      </div>

      <div className="flex-1 flex items-center justify-center min-h-[140px]">
        <div className="relative">
          <motion.p
            className="text-base text-[var(--color-neutral-500)] absolute inset-0 flex items-center justify-center"
            animate={{
              opacity: !isDeployed ? 1 : 0,
              scale: !isDeployed ? 1 : 0.8,
            }}
            transition={{ duration: 0.3 }}
          >
            Deploying...
          </motion.p>

          <motion.div
            className="flex flex-col items-center justify-center gap-2"
            animate={{
              opacity: isDeployed ? 1 : 0,
              scale: isDeployed ? 1 : 1.2,
            }}
            transition={{ duration: 0.3 }}
          >
            <AnimateNumber
              className="text-5xl font-mono font-light text-[var(--color-neutral-900)] tabular-nums leading-none"
              transition={{
                visualDuration: 1.2,
                type: "spring",
                bounce: 0.25,
              }}
            >
              {isDeployed ? TOOL_COUNT : 0}
            </AnimateNumber>
            <motion.p
              className="text-sm text-[var(--color-neutral-600)]"
              initial={{ opacity: 0, y: -10 }}
              animate={{
                opacity: isDeployed ? 1 : 0,
                y: isDeployed ? 0 : -10,
              }}
              transition={{ duration: 0.3, delay: 0.1 }}
            >
              tools
            </motion.p>
          </motion.div>
        </div>
      </div>

      <div className="text-center h-5">
        <motion.p
          className="text-sm text-[var(--color-neutral-600)]"
          animate={{
            opacity: isDeployed ? 1 : 0,
          }}
          transition={{ duration: 0.5 }}
        >
          <span className="font-mono text-[var(--color-success-700)]">
            12ms
          </span>{" "}
          response time
        </motion.p>
      </div>
    </div>
  );
}

function APIToolsSection() {
  const [hoveredFeature, setHoveredFeature] = useState<number>(-1);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0">
      <div className="border-r border-border flex items-center justify-center px-2 sm:px-8 py-8 sm:py-12 order-2 md:order-1">
        <AnimatedAPITransform activeFeature={hoveredFeature} />
      </div>
      <div className="flex flex-col justify-center px-2 sm:px-6 py-8 sm:py-12 order-1 md:order-2">
        <h2 className="font-display text-2xl sm:text-3xl md:text-4xl lg:text-display-lg mb-3 sm:mb-4">
          Create higher quality tools directly from your API
        </h2>
        <ul className="space-y-2 sm:space-y-3 text-sm sm:text-base text-foreground/60 mb-4 sm:mb-6">
          <li
            className="flex items-start gap-2 sm:gap-3 cursor-pointer"
            onMouseEnter={() => setHoveredFeature(0)}
            onMouseLeave={() => setHoveredFeature(-1)}
          >
            <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
              <Code2 className="w-4 h-4 text-black" />
            </div>
            <span>
              <span className="underline decoration-dotted underline-offset-2 hover:text-foreground transition-colors">
                Autogenerate tool definitions
              </span>{" "}
              from OpenAPI
            </span>
          </li>
          <li
            className="flex items-start gap-2 sm:gap-3 cursor-pointer"
            onMouseEnter={() => setHoveredFeature(1)}
            onMouseLeave={() => setHoveredFeature(-1)}
          >
            <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
              <Workflow className="w-4 h-4 text-black" />
            </div>
            <span>
              Craft{" "}
              <span className="underline decoration-dotted underline-offset-2 hover:text-foreground transition-colors">
                higher order tools
              </span>{" "}
              for complex agentic workflows
            </span>
          </li>
          <li className="flex items-start gap-2 sm:gap-3">
            <div className="w-6 h-6 rounded-[6px] border border-[#dcdcdc] flex items-center justify-center flex-shrink-0">
              <BookOpen className="w-4 h-4 text-black" />
            </div>
            <span>
              Catalog and distribute prompt templates to make your tools useful
              for everyone
            </span>
          </li>
        </ul>
      </div>
    </div>
  );
}

function AnimatedAPITransform({ activeFeature }: { activeFeature: number }) {
  const ref = useRef(null);
  const isInView = useInView(ref, { once: true, amount: 0.5 });
  const [hasTransformed, setHasTransformed] = useState(false);

  useEffect(() => {
    if (isInView && !hasTransformed) {
      setTimeout(() => {
        setHasTransformed(true);
      }, 600);
    }
  }, [isInView, hasTransformed]);

  // Determine state based on activeFeature prop
  // -1 or 0: Show basic transformation (OpenAPI -> Tools)
  // 1: Show higher order tools
  const showBasicTools = hasTransformed && activeFeature !== 1;
  const showHigherOrder = hasTransformed && activeFeature === 1;

  return (
    <div ref={ref} className="w-full max-w-sm">
      <motion.div
        className="relative h-[320px]"
        animate={{
          scale: isInView && !hasTransformed ? [1, 1.02, 1] : 1,
        }}
        transition={{
          duration: 1,
          repeat: isInView && !hasTransformed ? 2 : 0,
        }}
      >
        {/* OpenAPI Spec */}
        <motion.div
          className="absolute inset-0"
          animate={{
            opacity: !hasTransformed ? 1 : 0,
            scale: !hasTransformed ? 1 : 0.9,
            filter: !hasTransformed ? "blur(0px)" : "blur(8px)",
            rotateY: !hasTransformed ? 0 : -15,
          }}
          transition={{
            duration: 0.6,
            ease: [0.23, 1, 0.32, 1],
          }}
          style={{
            pointerEvents: !hasTransformed ? "auto" : "none",
            transformPerspective: 1000,
          }}
        >
          <div className="bg-[var(--color-neutral-900)] rounded-xl p-5 h-full font-mono text-[11px] overflow-hidden">
            <div className="text-[var(--color-neutral-400)] mb-2">
              openapi: 3.0.0
            </div>
            <div className="text-[var(--color-neutral-300)] space-y-2">
              <div>paths:</div>
              <div className="ml-3 space-y-2">
                <div>
                  {"/pet/:id"}:
                  <div className="ml-3">
                    <span className="text-[var(--color-brand-blue-400)]">
                      get
                    </span>
                    : findPetById
                  </div>
                </div>
                <div>
                  /pet:
                  <div className="ml-3 space-y-1">
                    <div>
                      <span className="text-[var(--color-brand-green-400)]">
                        post
                      </span>
                      : addPet
                    </div>
                    <div>
                      <span className="text-[var(--color-brand-yellow-400)]">
                        put
                      </span>
                      : updatePet
                    </div>
                  </div>
                </div>
                <div>
                  {"/pet/:id"}:
                  <div className="ml-3">
                    <span className="text-[var(--color-brand-red-400)]">
                      delete
                    </span>
                    : deletePet
                  </div>
                </div>
                <div>
                  /pet/findByStatus:
                  <div className="ml-3">
                    <span className="text-[var(--color-brand-blue-400)]">
                      get
                    </span>
                    : findByStatus
                  </div>
                </div>
              </div>
            </div>
          </div>
        </motion.div>

        {/* AI Tool */}
        <motion.div
          className="absolute inset-0"
          initial={{
            opacity: 0,
            scale: 0.8,
            rotateY: 15,
          }}
          animate={{
            opacity: hasTransformed ? 1 : 0,
            scale: hasTransformed ? 1 : 0.8,
            rotateY: hasTransformed ? 0 : 15,
          }}
          transition={{
            duration: 0.7,
            ease: [0.23, 1, 0.32, 1],
            delay: hasTransformed ? 0.15 : 0,
          }}
          style={{
            transformPerspective: 1000,
          }}
        >
          <div className="bg-background-pure rounded-xl border border-[var(--color-neutral-200)] p-4 h-full shadow-[0_8px_32px_-8px_rgba(0,0,0,0.12)] flex flex-col">
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-[var(--color-neutral-900)]">
                {showHigherOrder ? "Higher Order Tool" : "Auto-generated Tools"}
              </h4>
              <motion.div
                initial={{ scale: 0, rotate: -180 }}
                animate={{ scale: hasTransformed ? 1 : 0, rotate: 0 }}
                transition={{ type: "spring", delay: 0.7 }}
              >
                <div className="w-5 h-5 rounded-full bg-[var(--color-success-100)] flex items-center justify-center">
                  <CheckCircle className="w-3 h-3 text-[var(--color-success-600)]" />
                </div>
              </motion.div>
            </div>

            <div className="flex-1 flex flex-col">
              {/* Show either basic tools or higher order tool */}
              {!showHigherOrder ? (
                // Basic tools state
                <div className="space-y-1">
                  {showBasicTools &&
                    [
                      { name: "findPetById", desc: "GET /pet/{id}" },
                      { name: "addPet", desc: "POST /pet" },
                      { name: "updatePet", desc: "PUT /pet" },
                      { name: "deletePet", desc: "DELETE /pet/{id}" },
                      { name: "findByStatus", desc: "GET /pet/findByStatus" },
                    ].map((tool, index) => {
                      return (
                        <motion.div
                          key={tool.name}
                          initial={{ opacity: 0, x: -20 }}
                          animate={{
                            opacity: 1,
                            x: 0,
                          }}
                          transition={{
                            delay: 0.4 + index * 0.08,
                            duration: 0.3,
                          }}
                          className="px-2 py-1.5 rounded-lg hover:bg-[var(--color-neutral-50)] transition-colors cursor-pointer"
                        >
                          <div className="font-mono text-[11px] text-[var(--color-neutral-900)]">
                            {tool.name}
                          </div>
                          <div className="text-[10px] text-[var(--color-neutral-600)]">
                            {tool.desc}
                          </div>
                        </motion.div>
                      );
                    })}
                </div>
              ) : (
                // Higher order tool state
                <motion.div
                  initial={{ opacity: 0, y: 20 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.5, ease: [0.23, 1, 0.32, 1] }}
                  className="flex flex-col justify-center h-full"
                >
                  <div className="p-2.5 rounded-lg border-2 border-[var(--color-brand-blue-300)] bg-gradient-to-br from-[var(--color-brand-blue-50)] to-[var(--color-brand-blue-100)]">
                    <div className="font-mono text-xs text-[var(--color-neutral-900)] mb-1 flex items-center gap-2">
                      registerNewPet
                      <span className="text-[9px] px-1 py-0.5 rounded bg-[var(--color-brand-blue-300)] text-[var(--color-brand-blue-800)] font-sans font-medium">
                        Workflow
                      </span>
                    </div>
                    <div className="text-[10px] text-[var(--color-neutral-700)] mb-1.5">
                      Validates and registers a new pet in one call
                    </div>
                    <div className="text-[9px] text-[var(--color-neutral-600)] bg-white/50 rounded p-1.5 font-mono leading-relaxed">
                      <div>1. Check if pet exists → findPetById</div>
                      <div>2. Create pet record → addPet</div>
                      <div>3. Set initial status → updatePet</div>
                    </div>
                  </div>
                </motion.div>
              )}
            </div>
          </div>
        </motion.div>
      </motion.div>
    </div>
  );
}
