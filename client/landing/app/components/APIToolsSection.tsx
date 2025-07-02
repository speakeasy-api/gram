"use client";

import { useState, useRef, useEffect } from "react";
import { motion, useInView } from "framer-motion";
import { Code2, Workflow, BookOpen, CheckCircle } from "lucide-react";

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

export default function APIToolsSection() {
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
              to support complex agentic workflows
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