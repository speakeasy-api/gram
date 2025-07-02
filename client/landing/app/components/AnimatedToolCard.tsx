"use client";

import { useEffect, useRef, useState } from "react";
import { motion, useInView, AnimatePresence } from "framer-motion";
import { AnimateNumber } from "motion-plus/react";

export default function AnimatedToolCard() {
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