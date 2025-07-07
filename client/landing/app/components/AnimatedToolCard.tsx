"use client";

import { useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { AnimateNumber } from "motion-plus/react";
import { useBentoItemState } from "./BentoGrid";

type AnimationState = 'default' | 'story-button' | 'story-clicking' | 'story-deploying' | 'story-deployed';

export default function AnimatedToolCard() {
  const { isHovered } = useBentoItemState();
  const TOOL_COUNT = 17;
  const [animationState, setAnimationState] = useState<AnimationState>('default');
  
  // Show story mode when hovering OR when we've completed the deployment story
  const showStoryMode = isHovered || animationState === 'story-deployed';
  const showDefaultCard = !showStoryMode;

  // State machine for animation steps
  useEffect(() => {
    if (isHovered) {
      // Start the story sequence
      setAnimationState('story-button');
      
      // Step 1: Show button for 600ms
      const clickTimer = setTimeout(() => {
        setAnimationState('story-clicking');
        
        // Step 2: Button click animation for 200ms
        const deployTimer = setTimeout(() => {
          setAnimationState('story-deploying');
          
          // Step 3: Deploying for 1200ms
          const completeTimer = setTimeout(() => {
            setAnimationState('story-deployed');
          }, 1200);
          
          return () => clearTimeout(completeTimer);
        }, 200);
        
        return () => clearTimeout(deployTimer);
      }, 600);
      
      return () => clearTimeout(clickTimer);
    }
    // When hover ends, don't reset if we're in final deployed state
  }, [isHovered]);

  return (
    <div className="w-full max-w-sm">
      <div className="relative min-h-[140px]">
        <AnimatePresence mode="wait">
          {showDefaultCard ? (
            // Default: Show deployed server card
            <motion.div
              key="default-card"
              className="bg-white rounded-xl border border-neutral-200 overflow-hidden"
              initial={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              transition={{ duration: 0.15, ease: "easeInOut" }}
            >
              {/* Header */}
              <div className="flex items-center justify-between p-2 border-b border-neutral-200">
                <div className="flex items-center gap-2">
                  <div className="w-6 h-6 rounded-lg bg-neutral-100 flex items-center justify-center">
                    <span className="text-xs font-display text-neutral-700 relative top-[-1px]">
                      g
                    </span>
                  </div>
                  <h3 className="text-xs font-medium text-neutral-900">
                    MCP Server
                  </h3>
                </div>
                <div className="w-1.5 h-1.5 rounded-full bg-success-500 relative">
                  <motion.div
                    className="absolute inset-0 w-1.5 h-1.5 bg-success-500 rounded-full"
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
                </div>
              </div>

              {/* Content - Always show deployed state */}
              <div className="p-3">
                <div className="flex items-center justify-center min-h-[80px]">
                  <div className="text-center space-y-2">
                    <div>
                      <div className="text-2xl font-mono text-neutral-900 tabular-nums">
                        {TOOL_COUNT}
                      </div>
                      <div className="text-[10px] text-neutral-500 mt-0.5">
                        tools available
                      </div>
                    </div>

                    <div className="flex items-center justify-center gap-3 text-[10px]">
                      <div className="flex items-center gap-1">
                        <div className="w-0.5 h-0.5 rounded-full bg-success-500" />
                        <span className="text-neutral-600">
                          <span className="font-mono text-success-700">12ms</span> avg
                        </span>
                      </div>
                      <div className="flex items-center gap-1">
                        <div className="w-0.5 h-0.5 rounded-full bg-brand-blue-500" />
                        <span className="text-neutral-600">
                          <span className="font-mono text-brand-blue-700">99.9%</span> uptime
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </motion.div>
          ) : (
            // Story mode: Show button deployment flow
            <motion.div
              key="story-mode"
              className="flex items-center justify-center min-h-[140px]"
              initial={{ opacity: 0, scale: 1.05 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              transition={{ duration: 0.15, ease: "easeInOut" }}
            >
              {animationState === 'story-button' || animationState === 'story-clicking' || animationState === 'story-deploying' ? (
                // Show centered button
                <motion.button
                  className="relative px-4 py-1.5 rounded-full font-mono text-[10px] uppercase tracking-wider text-neutral-800 overflow-hidden"
                  key={animationState} // Force re-render on state change
                  initial={{ scale: 1 }}
                  animate={{
                    scale: animationState === 'story-clicking' ? 0.9 : 1,
                  }}
                  transition={{ duration: 0.2, ease: "easeInOut" }}
                >
                  {/* Rainbow border effect */}
                  <div className="absolute inset-0 p-[1px] rounded-full bg-gradient-primary -z-10" />
                  <div className="absolute inset-[1px] rounded-full bg-white -z-10" />
                  <span className="relative z-10">
                    {animationState === 'story-deploying' ? "Deploying..." : "Deploy Server"}
                  </span>
                </motion.button>
              ) : (
                // Show deployed card in story mode
                <motion.div
                  className="bg-white rounded-xl border border-neutral-200 overflow-hidden w-full"
                  initial={{ opacity: 0, scale: 0.9, y: -10 }}
                  animate={{ opacity: 1, scale: 1, y: 0 }}
                  transition={{ duration: 0.3, ease: [0.23, 1, 0.32, 1] }}
                >
                  {/* Same structure as default card but with animations */}
                  <div className="flex items-center justify-between p-2 border-b border-neutral-200">
                    <div className="flex items-center gap-2">
                      <div className="w-6 h-6 rounded-lg bg-neutral-100 flex items-center justify-center">
                        <span className="text-xs font-display text-neutral-700 relative top-[-1px]">g</span>
                      </div>
                      <h3 className="text-xs font-medium text-neutral-900">MCP Server</h3>
                    </div>
                    <motion.div
                      className="w-1.5 h-1.5 rounded-full bg-success-500 relative"
                      initial={{ backgroundColor: "#e5e5e5" }}
                      animate={{ backgroundColor: "#10b981" }}
                      transition={{ duration: 0.3 }}
                    >
                      <motion.div
                        className="absolute inset-0 w-1.5 h-1.5 bg-success-500 rounded-full"
                        animate={{ scale: [1, 2, 2], opacity: [0.8, 0, 0] }}
                        transition={{ duration: 2, repeat: Infinity, ease: "easeOut" }}
                      />
                    </motion.div>
                  </div>

                  <div className="p-3">
                    <div className="flex items-center justify-center min-h-[80px]">
                      <div className="text-center space-y-2">
                        <div>
                          <AnimateNumber
                            className="text-2xl font-mono text-neutral-900 tabular-nums"
                            transition={{ visualDuration: 1.2, type: "spring", bounce: 0.25 }}
                          >
                            {TOOL_COUNT}
                          </AnimateNumber>
                          <div className="text-[10px] text-neutral-500 mt-0.5">tools available</div>
                        </div>
                        <div className="flex items-center justify-center gap-3 text-[10px]">
                          <div className="flex items-center gap-1">
                            <div className="w-0.5 h-0.5 rounded-full bg-success-500" />
                            <span className="text-neutral-600">
                              <span className="font-mono text-success-700">12ms</span> avg
                            </span>
                          </div>
                          <div className="flex items-center gap-1">
                            <div className="w-0.5 h-0.5 rounded-full bg-brand-blue-500" />
                            <span className="text-neutral-600">
                              <span className="font-mono text-brand-blue-700">99.9%</span> uptime
                            </span>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </motion.div>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}
