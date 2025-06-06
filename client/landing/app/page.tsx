"use client";

import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import SpeakeasyLogo from "./components/SpeakeasyLogo";

interface Dot {
  id: string;
  x: number;
  y: number;
  size: number;
  delay: number;
}

export default function Home() {
  const [dots, setDots] = useState<Dot[]>([]);
  const [isResizing, setIsResizing] = useState(false);
  const resizeTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  const introducingRef = useRef<HTMLHeadingElement>(null);
  const gramRef = useRef<HTMLHeadingElement>(null);
  const descriptionRef = useRef<HTMLDivElement>(null);
  const buttonsRef = useRef<HTMLDivElement>(null);

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
    // "Introducing" text - include descenders with generous padding
    const introducingPadding = isMobile ? 20 : isTablet ? 25 : 30;
    const introducingDescenderExtra = isMobile ? 15 : isTablet ? 20 : 25; // Responsive extra space

    if (
      x >= introducingBounds.left - introducingPadding &&
      x <= introducingBounds.right + introducingPadding &&
      y >= introducingBounds.top - introducingPadding &&
      y <= introducingBounds.bottom + introducingDescenderExtra
    ) {
      return true;
    }

    // "gram." text - generous padding all around, extra for descender
    const gramPadding = isMobile ? 20 : isTablet ? 25 : 30;
    const gramDescenderExtra = isMobile ? 25 : isTablet ? 30 : 40; // More space for prominent 'g'

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
    const screenHeight = window.innerHeight;

    const paddingX = isMobile ? 24 : isTablet ? 40 : 160;

    const startX = paddingX;
    const startY = introducingBounds.top;
    const endX = screenWidth - (isMobile ? paddingX : screenWidth * 0.08);
    const endY = screenHeight - (isMobile ? 120 : 80);

    const cols = Math.ceil((endX - startX) / dotSpacing);
    const rows = Math.ceil((endY - startY) / (dotSpacing * 0.87));

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

        if (y < introducingBounds.bottom && x < introducingBounds.right + 20) {
          continue;
        }

        if (
          shouldSkipDot(
            x,
            y,
            introducingBounds,
            gramBounds,
            descriptionBounds,
            buttonsBounds || null,
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
    return () => {
      window.removeEventListener("resize", handleResize);
      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current);
      }
    };
  }, []);

  return (
    <>
      <header className="header-base backdrop-blur-[10px]">
        <div className="absolute top-0 left-0 right-0 h-1 w-full bg-gradient-primary" />
        <div className="flex items-center px-6 md:px-10 lg:px-40 pt-1">
          <a
            href="https://www.speakeasy.com/"
            className="transition-opacity hover:opacity-80"
          >
            <SpeakeasyLogo className="h-5 w-auto text-foreground" />
          </a>
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
            <motion.div
              key={dot.id}
              className="absolute"
              style={{
                width: dot.size,
                height: dot.size,
                left: dot.x,
                top: dot.y,
                x: "-50%",
                y: "-50%",
              }}
              initial={{ opacity: 0, scale: 0.5 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{
                duration: 0.6,
                delay: dot.delay,
                ease: "easeOut",
              }}
              whileHover={{ scale: 1.1 }}
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
                  className="hover:fill-neutral-50"
                />
              </svg>
            </motion.div>
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

        <div className="fixed bottom-8 left-6 right-6 md:bottom-10 md:right-10 lg:bottom-24 lg:right-24 md:left-auto z-30">
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
              <a
                className="relative rounded-full flex items-center justify-center h-[52px] px-5 font-mono font-light text-sm tracking-[0.03em] uppercase bg-[#FAFAFA] text-black transition-all hover:-translate-y-0.5 whitespace-nowrap overflow-hidden group"
                href="https://app.getgram.ai/login"
              >
                {/* Gradient border using pseudo-element */}
                <span className="absolute inset-0 rounded-full bg-gradient-primary p-[1px]">
                  <span className="flex h-full w-full items-center justify-center rounded-full bg-[#FAFAFA] group-hover:bg-[#F0F0F0]" />
                </span>
                <span className="relative z-10">Join the waitlist</span>
              </a>
              <a
                className="rounded-full flex items-center justify-center h-[52px] px-5 font-mono font-light text-sm tracking-[0.03em] uppercase bg-[#2F2F2F] text-white shadow-[inset_0px_2px_1px_#414141,inset_0px_-2px_1px_rgba(0,0,0,0.05)] transition-all hover:-translate-y-0.5 hover:bg-[#252525] whitespace-nowrap"
                style={{ textShadow: "0px 1px 1px rgba(26, 26, 26, 0.8)" }}
                href="https://calendly.com/sagar-speakeasy/30min"
              >
                Book a demo
              </a>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
