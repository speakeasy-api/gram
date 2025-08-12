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
import { Button } from "./components/Button";
import MCPGraphic from "./components/MCPGraphic";
import HowItWorksSection from "./components/HowItWorksSection";
import { BentoGrid, BentoGridRow, BentoGridItem } from "./components/BentoGrid";
import {
  Section,
  Container,
  Heading,
  Text,
  Flex,
  Grid,
  CommunityBadge,
  ButtonGroup,
  Badge,
} from "./components/sections";

export default function Home() {
  const [showNavbarCTA, setShowNavbarCTA] = useState(false);
  const [isMobile, setIsMobile] = useState(false);

  const buttonsRef = useRef<HTMLDivElement>(null);

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
      },
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
    <Flex direction="col" gap={0} className="min-h-screen">
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
            <Button
              href="https://docs.getgram.ai/"
              variant="primary-light"
              size="default"
            >
              View docs
            </Button>
            {!isMobile && (
              <Button
                href="https://app.getgram.ai/login"
                variant={showNavbarCTA ? "rainbow-light" : "primary-light"}
                size="default"
              >
                Login
              </Button>
            )}
          </motion.div>
        </div>
      </header>

      <Flex
        direction="col"
        gap={0}
        className="flex-1 space-y-16 sm:space-y-20 lg:space-y-24"
      >
        <Section size="hero" background="neutral">
          <Container>
            <Grid cols="hero" gap={12} align="center" className="lg:gap-16">
              <Flex direction="col" gap={6} className="lg:gap-8">
                <Badge variant="gradient">Introducing gram</Badge>

                <Heading size="hero" weight="light">
                  <span className="block">Your API.</span>
                  <span className="block">A hosted MCP Server.</span>
                  <span className="block">One click.</span>
                </Heading>

                {/* MCP Graphic on mobile - between title and description */}
                <div className="lg:hidden relative h-[280px] sm:h-[320px] flex items-center justify-center -mx-4">
                  <MCPGraphic />
                </div>

                <Flex direction="col" gap={8} className="lg:gap-12">
                  <Text
                    size="hero"
                    color="muted"
                    leading="relaxed"
                    className="max-w-xl"
                  >
                    Create, curate and distribute tools for AI. Everything you
                    need to power integrations for Agents and LLMs.
                  </Text>

                  <div ref={buttonsRef}>
                    <ButtonGroup
                      buttons={[
                        {
                          text: "Login",
                          href: "https://app.getgram.ai/login",
                          variant: "rainbow-light",
                          size: "chunky",
                        },
                        {
                          text: "Book a demo",
                          href: "https://www.speakeasy.com/book-demo",
                          variant: "primary-dark",
                          size: "chunky",
                        },
                      ]}
                    />
                  </div>
                </Flex>
              </Flex>

              {/* MCP Graphic on desktop - right column */}
              <div className="hidden lg:flex lg:col-start-2 lg:row-start-1 lg:row-span-1 relative h-[600px] xl:h-[700px] items-center justify-center">
                <MCPGraphic />
              </div>
            </Grid>
          </Container>
        </Section>

        <HowItWorksSection />

        <Section
          background="neutral"
          size="none"
          className="pb-8 sm:pb-12 lg:pb-16"
        >
          <Container>
            <Flex
              direction="col"
              align="center"
              className="text-center mb-12 sm:mb-16"
            >
              <Heading size="display" align="center" className="mb-4 sm:mb-6">
                Everything you need to build with MCP
              </Heading>
              <Text
                size="description"
                color="muted"
                align="center"
                className="max-w-2xl mx-auto"
              >
                A complete platform for creating, hosting, and distributing AI
                tools at scale
              </Text>
            </Flex>

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
          </Container>
        </Section>

        <Section
          background="black"
          size="none"
          className="py-12 sm:py-16 lg:py-20 overflow-hidden"
        >
          <Container>
            <Flex direction="col" align="center" className="text-center">
              <Heading
                size="display"
                color="white"
                className="mb-8 sm:mb-12 max-w-4xl mx-auto text-center"
              >
                Build AI that works. Unlock API and Data for Agents. Secure and
                Composable.
              </Heading>
              <GramEcosystemAnimation />
            </Flex>
          </Container>
        </Section>

        <Section
          background="neutral"
          size="none"
          className="pb-20 sm:pb-24 lg:pb-28"
          asChild
        >
          <footer
            ref={footerRef}
            className="relative overflow-hidden flex flex-col justify-center items-center"
          >
            <Container size="2xl" className="relative z-20 pointer-events-none">
              <Flex direction="col" align="center">
                <CommunityBadge className="mb-8 sm:mb-12 pointer-events-auto" />

                <Heading
                  size="display"
                  weight="light"
                  align="center"
                  className="mb-10 sm:mb-12 max-w-3xl pointer-events-auto"
                >
                  Can&apos;t get enough MCP?
                </Heading>

                <div className="pointer-events-auto">
                  <ButtonGroup
                    buttons={[
                      {
                        text: "Join our Slack",
                        href: "https://go.speakeasy.com/slack",
                        variant: "rainbow-light",
                      },
                      {
                        text: "Read MCP docs",
                        href: "https://docs.getgram.ai/",
                      },
                    ]}
                  />
                </div>
              </Flex>
            </Container>

            <div className="absolute left-0 right-0 bottom-0 h-1 w-full bg-gradient-primary z-20" />
          </footer>
        </Section>

        {/* Traditional Footer */}
        <footer className="bg-neutral-100">
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
      </Flex>

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
              href="https://app.getgram.ai/login"
              className="w-full max-w-xs shadow-lg text-base py-4"
            >
              Login
            </Button>
          </motion.div>
        )}
      </AnimatePresence>
    </Flex>
  );
}
