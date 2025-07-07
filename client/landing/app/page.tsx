"use client";

import { useEffect, useRef, useState } from "react";
import { motion, useInView, AnimatePresence } from "framer-motion";
import SpeakeasyLogo from "./components/SpeakeasyLogo";
import PlugAndPlayLogos from "./components/PlugAndPlayLogos";
import GramEcosystemAnimation from "./components/GramEcosystemAnimation";
import AnimatedToolCard from "./components/AnimatedToolCard";
import CurateToolsetsAnimation from "./components/CurateToolsetsAnimation";
import StackedMetricCards from "./components/StackedMetricCards";
import { AnimatedAPITransform } from "./components/APIToolsSection";
import { Button, buttonVariants } from "./components/Button";
import MCPGraphic from "./components/MCPGraphic";
import HowItWorksSection from "./components/HowItWorksSection";
import { BentoGrid, BentoGridRow, BentoGridItem } from "./components/BentoGrid";

export default function Home() {
  const [showNavbarCTA, setShowNavbarCTA] = useState(false);
  const [isMobile, setIsMobile] = useState(false);

  const introducingRef = useRef<HTMLHeadingElement>(null);
  const descriptionRef = useRef<HTMLDivElement>(null);
  const buttonsRef = useRef<HTMLDivElement>(null);

  const footerHeadingRef = useRef<HTMLHeadingElement>(null);
  const footerButtonsRef = useRef<HTMLDivElement>(null);

  const footerRef = useRef<HTMLDivElement>(null);
  const isFooterInView = useInView(footerRef, { amount: 0.1 });

  useEffect(() => {
    const heroObserver = new window.IntersectionObserver(
      ([entry]) => {
        setShowNavbarCTA(!entry.isIntersecting);
      },
      {
        threshold: 0,
        rootMargin: "-80px 0px 0px 0px",
      }
    );

    const currentButtonsRef = buttonsRef.current;
    if (currentButtonsRef) {
      heroObserver.observe(currentButtonsRef);
    }

    const checkMobile = () => {
      setIsMobile(window.innerWidth < 768);
    };
    checkMobile();
    window.addEventListener("resize", checkMobile);
    return () => {
      window.removeEventListener("resize", checkMobile);
      if (currentButtonsRef) {
        heroObserver.unobserve(currentButtonsRef);
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

      <div className="relative min-h-[80vh] flex items-center py-16 lg:py-20">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-12 lg:gap-16 items-center">
            {/* Left column - Text content */}
            <div className="flex flex-col gap-6 lg:gap-8 py-8 lg:py-0">
              {/* MCP Badge */}
              <div className="inline-flex items-center">
                <span
                  className="relative inline-flex items-center px-2 py-1 text-xs font-mono text-neutral-700 uppercase tracking-wider rounded-xs"
                  style={{
                    background:
                      "linear-gradient(white, white) padding-box, linear-gradient(90deg, var(--gradient-brand-primary-colors)) border-box",
                    border: "1px solid transparent",
                  }}
                >
                  Introducing gram
                </span>
              </div>

              <div className="space-y-2">
                <h1
                  ref={introducingRef}
                  className="font-display font-light text-4xl sm:text-5xl md:text-6xl lg:text-6xl xl:text-7xl leading-[1.1] tracking-tight text-neutral-900"
                >
                  <span className="block">Your API.</span>
                  <span className="block">A hosted MCP Server.</span>
                  <span className="block">One click.</span>
                </h1>
              </div>

              <div ref={descriptionRef} className="space-y-10 lg:space-y-12">
                <p className="text-neutral-600 text-lg md:text-xl lg:text-2xl leading-[1.6] max-w-xl">
                  Create, curate and distribute tools for AI. Everything you
                  need to power integrations for Agents and LLMs.
                </p>

                <div
                  ref={buttonsRef}
                  className="flex flex-col sm:flex-row gap-4"
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

            {/* Single MCP Graphic - responsive positioning */}
            <div className="lg:col-start-2 lg:row-start-1 lg:row-span-1 relative h-[400px] md:h-[500px] lg:h-[600px] xl:h-[700px] flex items-center justify-center">
              <MCPGraphic />
            </div>
          </div>
        </div>
      </div>

      <HowItWorksSection />

      <section className="w-full py-24 sm:py-32 lg:py-40 relative">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8">
          <div className="text-center mb-12 sm:mb-16">
            <h2 className="text-display-sm sm:text-display-md lg:text-display-lg mb-4">
              Everything you need to build with MCP
            </h2>
            <p className="text-base sm:text-lg text-neutral-600 max-w-2xl mx-auto">
              A complete platform for creating, hosting, and distributing AI
              tools at scale
            </p>
          </div>

          <BentoGrid className="w-full">
            {/* First row - 2 columns */}
            <BentoGridRow columns={2}>
              <BentoGridItem
                title="Easiest way to host MCP at scale"
                description="Deploy MCP servers with one click, redeploy new versions with zero downtime, and continuously test and refine tools for optimal performance. High quality Agentic Tools with Enterprise Experience."
                visual={<AnimatedToolCard />}
              />

              <BentoGridItem
                title="Curate Toolsets for every usecase"
                description="Group tools into focused toolsets, remix them across your APIs and third-party services, scope access by team, and instantly test for quality. Organize and optimize your tools for different workflows."
                visual={<CurateToolsetsAnimation />}
              />
            </BentoGridRow>

            {/* Second row - 3 columns */}
            <BentoGridRow columns={3} isLastRow={true}>
              <BentoGridItem
                title="Transform any API into AI Tools"
                description="Autogenerate tool definitions from OpenAPI specs, create higher order tools for complex workflows, and catalog your tools for easy distribution."
                visual={<AnimatedAPITransform />}
                visualSize="compact"
              />

              <BentoGridItem
                title="Enterprise-grade tool distribution"
                description="Model and framework agnostic gateway with managed authentication, built-in telemetry, audit logs, and rate limiting for secure tool distribution."
                visual={<StackedMetricCards />}
                visualSize="compact"
              />

              <BentoGridItem
                title="Plug & Play AI Integration"
                description="Interactive playground interface with seamless OAuth integration and ready-to-use landing pages. Zero integration required with popular AI clients."
                visual={<PlugAndPlayLogos />}
                visualSize="compact"
              />
            </BentoGridRow>
          </BentoGrid>
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
        <div className="relative z-20 w-full pointer-events-none">
          <div className="flex flex-col items-center justify-center py-32 sm:py-40 lg:py-48 max-w-2xl mx-auto px-4">
            {/* Community Badge */}
            <div className="inline-flex items-center gap-3 mb-8 sm:mb-12 pointer-events-auto">
              <div className="flex -space-x-2">
                <div className="w-8 h-8 rounded border-2 border-white bg-neutral-300"></div>
                <div className="w-8 h-8 rounded border-2 border-white bg-neutral-400"></div>
                <div className="w-8 h-8 rounded border-2 border-white bg-neutral-500"></div>
              </div>
              <span className="text-sm text-neutral-600">
                Join the community
              </span>
            </div>

            <h3
              ref={footerHeadingRef}
              className="text-display-sm sm:text-display-md lg:text-display-lg font-display font-light text-neutral-900 mb-10 sm:mb-12 text-center max-w-3xl pointer-events-auto"
            >
              Can&apos;t get enough MCP?
            </h3>

            <div
              ref={footerButtonsRef}
              className="flex flex-col md:flex-row gap-4 w-full md:w-auto justify-center pointer-events-auto"
            >
              <Button
                variant="rainbow-light"
                href="https://go.speakeasy.com/slack"
              >
                Join our Slack
              </Button>
              <Button href="https://docs.getgram.ai/">Read MCP docs</Button>
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
