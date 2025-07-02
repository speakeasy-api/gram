"use client";

import { useEffect, useRef, useState } from "react";
import {
  motion,
  useMotionValue,
  useInView,
  AnimatePresence,
} from "framer-motion";
import SpeakeasyLogo from "./components/SpeakeasyLogo";
import PlugAndPlayLogos from "./components/PlugAndPlayLogos";
import GramEcosystemAnimation from "./components/GramEcosystemAnimation";
import StackedMetricCards from "./components/StackedMetricCards";
import AnimatedToolCard from "./components/AnimatedToolCard";
import CurateToolsetsAnimation from "./components/CurateToolsetsAnimation";
import APIToolsSection from "./components/APIToolsSection";
import { Button, buttonVariants } from "./components/Button";
import { GridOverlay } from "./components/GridOverlay";
import FooterDotsHeroLike from "./components/FooterDotsHeroLike";
import HeroDotGrid from "./components/HeroDotGrid";
import HowItWorksSection from "./components/HowItWorksSection";
import {
  Zap,
  Key,
  Activity,
  Layers,
  Shuffle,
  Users,
  CheckCircle,
} from "lucide-react";

export default function Home() {
  const [isResizing, setIsResizing] = useState(false);
  const [active, setActive] = useState({ row: 0, col: 0 });
  const resizeTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const [showNavbarCTA, setShowNavbarCTA] = useState(false);
  const [isMobile, setIsMobile] = useState(false);

  const dragX = useMotionValue(0);
  const dragY = useMotionValue(0);

  const introducingRef = useRef<HTMLHeadingElement>(null);
  const gramRef = useRef<HTMLHeadingElement>(null);
  const mcpTextRef = useRef<HTMLDivElement>(null);
  const descriptionRef = useRef<HTMLDivElement>(null);
  const buttonsRef = useRef<HTMLDivElement>(null);

  const footerHeadingRef = useRef<HTMLHeadingElement>(null);
  const footerDescRef = useRef<HTMLParagraphElement>(null);
  const footerButtonsRef = useRef<HTMLDivElement>(null);

  const footerRef = useRef<HTMLDivElement>(null);
  const isFooterInView = useInView(footerRef, { amount: 0.1 });

  useEffect(() => {
    const handleResize = () => {
      setIsResizing(true);

      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current);
      }

      resizeTimeoutRef.current = setTimeout(() => {
        setIsResizing(false);
      }, 250);
    };

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
            target="_blank"
            rel="noopener noreferrer"
            className="transition-opacity hover:opacity-80"
          >
            <SpeakeasyLogo className="h-5 w-auto text-foreground" />
          </a>
          <motion.div
            className="flex items-center gap-3"
            layout
            transition={{
              layout: {
                type: "spring",
                stiffness: 300,
                damping: 30,
              },
            }}
          >
            <motion.a
              layout="position"
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
                  "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(220,220,220,0.2) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
              }}
              whileHover={{
                backgroundColor: "rgb(240 240 240)",
                boxShadow:
                  "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(210,210,210,0.3) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
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
                layout
                className="relative overflow-hidden"
                transition={{
                  layout: {
                    type: "spring",
                    stiffness: 300,
                    damping: 30,
                  },
                }}
              >
                <motion.a
                  href={
                    showNavbarCTA
                      ? "https://speakeasyapi.typeform.com/to/h6WJdwWr"
                      : "https://app.getgram.ai/auth/login"
                  }
                  className={
                    showNavbarCTA
                      ? buttonVariants({
                          variant: "rainbow-light",
                          size: "default",
                        }) + " shadow-lg"
                      : "relative inline-flex items-center justify-center font-mono text-[15px] leading-[1.6] tracking-[0.01em] uppercase whitespace-nowrap rounded-full transition-all"
                  }
                  initial={{
                    backgroundColor: "rgb(245 245 245)",
                    color: "rgb(38 38 38)",
                    paddingLeft: "20px",
                    paddingRight: "20px",
                    paddingTop: "8px",
                    paddingBottom: "8px",
                    boxShadow:
                      "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(220,220,220,0.2) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
                  }}
                  animate={{
                    backgroundColor: showNavbarCTA
                      ? "transparent"
                      : "rgb(245 245 245)",
                    color: "rgb(38 38 38)",
                    paddingLeft: "20px",
                    paddingRight: "20px",
                    paddingTop: showNavbarCTA ? "10px" : "8px",
                    paddingBottom: showNavbarCTA ? "10px" : "8px",
                    boxShadow: showNavbarCTA
                      ? "none"
                      : "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(220,220,220,0.2) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
                  }}
                  whileHover={{
                    backgroundColor: showNavbarCTA
                      ? "transparent"
                      : "rgb(240 240 240)",
                    boxShadow: showNavbarCTA
                      ? "none"
                      : "0px 2px 1px 0px #F3F3F3 inset, 0px -40px 10px 10px rgba(210,210,210,0.3) inset, 0px -2px 1px 0px rgba(0,0,0,0.05) inset",
                  }}
                  transition={{
                    type: "spring",
                    stiffness: 400,
                    damping: 30,
                  }}
                >
                  <span className="block invisible">
                    {showNavbarCTA ? "Join the waitlist" : "Login"}
                  </span>
                  <AnimatePresence mode="wait" initial={false}>
                    <motion.span
                      key={showNavbarCTA ? "waitlist" : "login"}
                      className="absolute inset-0 flex items-center justify-center"
                      initial={{ opacity: 0, filter: "blur(4px)" }}
                      animate={{ opacity: 1, filter: "blur(0px)" }}
                      exit={{ opacity: 0, filter: "blur(4px)" }}
                      transition={{ duration: 0.2 }}
                    >
                      {showNavbarCTA ? "Join the waitlist" : "Login"}
                    </motion.span>
                  </AnimatePresence>
                </motion.a>
              </motion.div>
            )}
          </motion.div>
        </div>
      </header>

      <div className="relative min-h-screen">
        <HeroDotGrid
          isResizing={isResizing}
          active={active}
          setActive={setActive}
          dragX={dragX}
          dragY={dragY}
          introducingRef={introducingRef}
          gramRef={gramRef}
          mcpTextRef={mcpTextRef}
          descriptionRef={descriptionRef}
          buttonsRef={buttonsRef}
        />

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

          {/* <div
            ref={mcpTextRef}
            className="absolute top-[65vh] left-6 md:left-10 lg:left-40 text-foreground/80 text-sm md:text-base lg:text-[1.0625rem] leading-relaxed"
          >
            <p>Your API → Hosted MCP, Instantly.</p>
            <p>Make your product LLM-ready.</p>
            <p>Connect your OpenAPI spec to launch</p>
            <p>a hosted MCP with zero maintenance.</p>
          </div> */}
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

      <HowItWorksSection />

      <section className="w-full py-24 sm:py-32 lg:py-40 relative">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8 relative">
          {/* Center divider line - moved outside to span full section */}
          <div className="absolute left-1/2 top-0 bottom-0 w-px bg-neutral-300 -translate-x-1/2 hidden md:block" />

          {/* Decoration squares where center line meets container top/bottom */}
          <div
            className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-20"
            style={{
              width: 10,
              height: 10,
              background: "var(--color-background)",
              border: "2px solid var(--color-neutral-300)",
              borderRadius: 2,
            }}
          />
          <div
            className="absolute left-1/2 bottom-0 -translate-x-1/2 translate-y-1/2 hidden md:block z-20"
            style={{
              width: 10,
              height: 10,
              background: "var(--color-background)",
              border: "2px solid var(--color-neutral-300)",
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
                    <span>1-click hosting of MCP servers.</span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Key className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Redeploy new versions of tools with zero downtime
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>Test and refine tools for optimal performance</span>
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
                  background: "var(--color-neutral-300)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-300)",
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
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
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
                  background: "var(--color-neutral-300)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-300)",
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
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
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
                    <span>Instantly test and refine tools for quality</span>
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
                  background: "var(--color-neutral-300)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-300)",
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
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
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
                  Secure and scalable Tools Gateway.
                </p>
                <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Zap className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Model and Framework Agnostic. Works with your stack.
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Key className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Support for managed and passthrough MCP authentication
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Built in telemetry, audit logs, and rate limiting
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Deploy and run on your own infrastructure for full control
                      over data and compliance requirements.
                    </span>
                  </li>
                </ul>
              </div>
              <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <StackedMetricCards />
              </div>
            </div>

            {/* Section divider */}
            <div className="relative h-0">
              {/* Left horizontal line - extends to center + 15px */}
              <div
                className="absolute left-0 top-0 h-px hidden md:block"
                style={{
                  right: "calc(50% - 15px)",
                  background: "var(--color-neutral-300)",
                }}
              />
              {/* Right horizontal line - extends from center + 15px */}
              <div
                className="absolute right-0 top-0 h-px hidden md:block"
                style={{
                  left: "calc(50% - 15px)",
                  background: "var(--color-neutral-300)",
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
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
              <div
                className="absolute right-0 top-0 translate-x-1/2 -translate-y-1/2 hidden md:block z-10"
                style={{
                  width: 10,
                  height: 10,
                  background: "var(--color-background)",
                  border: "2px solid var(--color-neutral-300)",
                  borderRadius: 2,
                }}
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-8 md:gap-0 min-h-[400px] md:min-h-[500px]">
              <div className="flex flex-col justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4 sm:mb-6 max-w-3xl">
                  Plug & Play AI Integration
                </h2>
                <p className="text-base sm:text-lg text-neutral-600 mb-6 sm:mb-8">
                  Users can interact with your product directly through any
                  popular AI client with zero integration.
                </p>
                <ul className="space-y-3 sm:space-y-4 text-base sm:text-lg text-neutral-900">
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Activity className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Test and develop your MCP integrations with our
                      interactive playground interface.
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Key className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Seamlessly integrates with your existing OAuth providers
                      and authentication systems.
                    </span>
                  </li>
                  <li className="flex items-start gap-3">
                    <div className="w-6 h-6 rounded-[6px] border border-neutral-300 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <Zap className="w-4 h-4 text-neutral-900" />
                    </div>
                    <span>
                      Out of the box landing page for your official MCP
                      endpoint.
                    </span>
                  </li>
                </ul>
              </div>
              <div className="flex items-center justify-center px-4 sm:px-8 lg:px-12 py-12 sm:py-16 lg:py-20">
                <PlugAndPlayLogos />
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

      {/* Traditional Footer */}
      <footer className="bg-neutral-100 border-t border-neutral-200">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8 pt-12 pb-6">
          {/* Main Content */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-8 mb-16">
            {/* Logo and Social - spans 2 columns on desktop */}
            <div className="col-span-1 md:col-span-2">
              {/* Mobile Layout */}
              <div className="flex justify-between items-start md:hidden mb-6">
                <a
                  href="https://www.speakeasy.com/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="transition-opacity hover:opacity-80"
                >
                  <SpeakeasyLogo className="h-5 w-auto text-foreground" />
                </a>
                <div className="flex gap-3">
                  <a
                    href="https://twitter.com/speakeasyapi"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-neutral-600 hover:text-neutral-900 transition-colors"
                  >
                    <svg
                      className="w-5 h-5"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                    </svg>
                  </a>
                  <a
                    href="https://linkedin.com/company/speakeasyapi"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-neutral-600 hover:text-neutral-900 transition-colors"
                  >
                    <svg
                      className="w-5 h-5"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path d="M19 0h-14c-2.761 0-5 2.239-5 5v14c0 2.761 2.239 5 5 5h14c2.762 0 5-2.239 5-5v-14c0-2.761-2.238-5-5-5zm-11 19h-3v-11h3v11zm-1.5-12.268c-.966 0-1.75-.79-1.75-1.764s.784-1.764 1.75-1.764 1.75.79 1.75 1.764-.783 1.764-1.75 1.764zm13.5 12.268h-3v-5.604c0-3.368-4-3.113-4 0v5.604h-3v-11h3v1.765c1.396-2.586 7-2.777 7 2.476v6.759z" />
                    </svg>
                  </a>
                </div>
              </div>

              {/* Desktop Layout */}
              <div className="hidden md:block">
                <a
                  href="https://www.speakeasy.com/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-block transition-opacity hover:opacity-80 mb-6 -ml-1"
                >
                  <SpeakeasyLogo className="h-5 w-auto text-foreground" />
                </a>
                <div className="flex items-center gap-3 text-sm text-neutral-600">
                  <span>Follow us on:</span>
                  <a
                    href="https://twitter.com/speakeasyapi"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-neutral-600 hover:text-neutral-900 transition-colors"
                  >
                    <svg
                      className="w-5 h-5"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                    </svg>
                  </a>
                  <a
                    href="https://linkedin.com/company/speakeasyapi"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-neutral-600 hover:text-neutral-900 transition-colors"
                  >
                    <svg
                      className="w-5 h-5"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path d="M19 0h-14c-2.761 0-5 2.239-5 5v14c0 2.761 2.239 5 5 5h14c2.762 0 5-2.239 5-5v-14c0-2.761-2.238-5-5-5zm-11 19h-3v-11h3v11zm-1.5-12.268c-.966 0-1.75-.79-1.75-1.764s.784-1.764 1.75-1.764 1.75.79 1.75 1.764-.783 1.764-1.75 1.764zm13.5 12.268h-3v-5.604c0-3.368-4-3.113-4 0v5.604h-3v-11h3v1.765c1.396-2.586 7-2.777 7 2.476v6.759z" />
                    </svg>
                  </a>
                </div>
              </div>
            </div>

            {/* Legal Links */}
            <div className="col-span-1">
              <h3 className="text-sm font-medium mb-4 text-neutral-600">
                Legal
              </h3>
              <ul className="space-y-2">
                <li>
                  <a
                    href="https://www.speakeasy.com/legal"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-neutral-900 hover:text-neutral-600 transition-colors"
                  >
                    Legal
                  </a>
                </li>
                <li>
                  <a
                    href="https://www.speakeasy.com/privacy"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-neutral-900 hover:text-neutral-600 transition-colors"
                  >
                    Privacy Policy
                  </a>
                </li>
                <li>
                  <a
                    href="https://www.speakeasy.com/terms"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-neutral-900 hover:text-neutral-600 transition-colors"
                  >
                    Terms of Service
                  </a>
                </li>
              </ul>
            </div>

            {/* Resources */}
            <div className="col-span-1">
              <h3 className="text-sm font-medium mb-4 text-neutral-600">
                Resources
              </h3>
              <ul className="space-y-2">
                <li>
                  <a
                    href="https://docs.getgram.ai/"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-neutral-900 hover:text-neutral-600 transition-colors"
                  >
                    Gram Docs
                  </a>
                </li>
                <li>
                  <a
                    href="https://www.speakeasy.com/mcp"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-neutral-900 hover:text-neutral-600 transition-colors"
                  >
                    Speakeasy MCP Hub
                  </a>
                </li>
              </ul>
            </div>
          </div>
        </div>

        {/* Bottom Section - Copyright */}
        <div className="flex flex-row justify-between items-center px-4 sm:px-6 lg:px-8 py-6">
          <p className="text-xs text-neutral-500">© 2025 Speakeasy</p>
          <p className="text-xs text-neutral-500">All rights reserved.</p>
        </div>
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
