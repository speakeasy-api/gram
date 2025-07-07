"use client";

import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { Code2, BookOpen, CheckCircle, Workflow } from "lucide-react";
import { useBentoItemState } from "./BentoGrid";

type APIAnimationState = 'default' | 'story-spec' | 'story-transforming' | 'story-complete';

function AnimatedAPITransform() {
  const { isHovered } = useBentoItemState();
  const [animationState, setAnimationState] = useState<APIAnimationState>('default');
  
  // State machine for animation
  useEffect(() => {
    if (isHovered) {
      setAnimationState('story-spec');
      
      const transformTimer = setTimeout(() => {
        setAnimationState('story-transforming');
        
        const completeTimer = setTimeout(() => {
          setAnimationState('story-complete');
        }, 800);
        
        return () => clearTimeout(completeTimer);
      }, 1000);
      
      return () => clearTimeout(transformTimer);
    }
  }, [isHovered]);

  // Show story mode when hovering OR when animation is complete
  const showStoryMode = isHovered || animationState === 'story-complete';

  return (
    <div className="w-full max-w-sm">
      <div className="relative h-[160px]">
        
        {!showStoryMode ? (
          // Default: Show tools with blurred spec in background
          <>
            {/* Background: Blurred OpenAPI Spec - more visible */}
            <div
              className="absolute left-[10%] top-1/2 -translate-y-1/2 w-[55%] bg-gradient-to-br from-neutral-100 to-neutral-50 rounded-lg overflow-hidden border border-neutral-200"
              style={{
                zIndex: 1,
                filter: "blur(1px)",
                opacity: 0.5,
              }}
            >
              <div className="flex items-center gap-1.5 p-1.5 bg-white border-b border-neutral-200">
                <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
                  <rect x="2" y="3" width="12" height="10" rx="1" className="stroke-neutral-400" strokeWidth="1.5" />
                  <path d="M5 6.5H11M5 9.5H9" className="stroke-neutral-400" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
                <span className="text-[8px] font-medium text-neutral-700">PETSTORE.YAML</span>
              </div>
              <div className="p-2 font-mono text-[7px] leading-[1.2] space-y-0.5">
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">1</span>
                  <span className="text-brand-green-600">openapi</span>
                  <span className="text-neutral-600">: </span>
                  <span className="text-brand-blue-600">3.0.0</span>
                </div>
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">2</span>
                  <span className="text-brand-green-600">paths</span>
                </div>
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">3</span>
                  <span className="ml-1 text-brand-green-600">/pet</span>
                </div>
              </div>
            </div>

            {/* Foreground: Tools card */}
            <div className="absolute right-[10%] top-1/2 -translate-y-1/2 w-[55%] z-10">
              <div className="w-full bg-white rounded-lg overflow-hidden border border-neutral-200">
                <div className="flex items-center justify-between p-1.5 border-b border-neutral-200">
                  <h4 className="text-[8px] font-medium text-neutral-900">Auto-generated Tools</h4>
                  <CheckCircle className="w-3 h-3 text-success-600" />
                </div>
                <div className="p-2 overflow-hidden">
                  <div className="space-y-1 overflow-hidden">
                    {[
                      { name: "findPetById", desc: "GET /pet/{id}", color: "blue" },
                      { name: "deletePet", desc: "DELETE /pet/{id}", color: "red" },
                      { name: "addPet", desc: "POST /pet", color: "green" },
                    ].map((tool) => (
                      <div key={tool.name} className="flex items-center gap-2 p-1 rounded-md">
                        <div className={`w-1 h-1 rounded-full ${
                          tool.color === "blue" ? "bg-brand-blue-500" :
                          tool.color === "green" ? "bg-brand-green-500" :
                          tool.color === "red" ? "bg-brand-red-500" : ""
                        }`} />
                        <div className="flex-1">
                          <div className="font-mono text-[8px] text-neutral-900">{tool.name}</div>
                          <div className="text-[7px] text-neutral-500">{tool.desc}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          </>
        ) : (
          // Story mode: Show transformation animation
          <>
            {/* OpenAPI Spec */}
            <motion.div
              className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-[55%] bg-gradient-to-br from-neutral-100 to-neutral-50 rounded-lg overflow-hidden border border-neutral-200"
              animate={{
                x: animationState === 'story-spec' ? 0 : animationState === 'story-transforming' ? -30 : animationState === 'story-complete' ? -80 : 0,
                scale: animationState === 'story-transforming' ? 0.9 : 1,
                opacity: animationState === 'story-complete' ? 0.5 : 1,
                filter: animationState === 'story-complete' ? "blur(1px)" : "blur(0px)",
              }}
              transition={{ duration: 0.5, ease: "easeInOut" }}
              style={{ zIndex: animationState === 'story-spec' ? 10 : 1 }}
            >
              <div className="flex items-center gap-1.5 p-1.5 bg-white border-b border-neutral-200">
                <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
                  <rect x="2" y="3" width="12" height="10" rx="1" className="stroke-neutral-400" strokeWidth="1.5" />
                  <path d="M5 6.5H11M5 9.5H9" className="stroke-neutral-400" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
                <span className="text-[8px] font-medium text-neutral-700">PETSTORE.YAML</span>
              </div>
              <div className="p-2 font-mono text-[7px] leading-[1.2] space-y-0.5">
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">1</span>
                  <span className="text-brand-green-600">openapi</span>
                  <span className="text-neutral-600">: </span>
                  <span className="text-brand-blue-600">3.0.0</span>
                </div>
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">2</span>
                  <span className="text-brand-green-600">paths</span>
                </div>
                <div className="flex">
                  <span className="text-neutral-400 mr-1 select-none">3</span>
                  <span className="ml-1 text-brand-green-600">/pet</span>
                </div>
              </div>
            </motion.div>

            {/* AI Tools */}
            <motion.div
              className="absolute right-[10%] top-1/2 -translate-y-1/2 w-[55%]"
              initial={{ opacity: 0, x: 20 }}
              animate={{
                opacity: animationState === 'story-transforming' || animationState === 'story-complete' ? 1 : 0,
                x: animationState === 'story-transforming' || animationState === 'story-complete' ? 0 : 20,
                scale: animationState === 'story-complete' ? 1 : 0.95,
              }}
              transition={{ duration: 0.4, ease: "easeOut" }}
              style={{ zIndex: 10 }}
            >
              <div className="w-full bg-white rounded-lg overflow-hidden border border-neutral-200">
                <div className="flex items-center justify-between p-1.5 border-b border-neutral-200">
                  <h4 className="text-[8px] font-medium text-neutral-900">Auto-generated Tools</h4>
                  <motion.div
                    initial={{ scale: 0 }}
                    animate={{ 
                      scale: animationState === 'story-complete' ? 1 : 0,
                      rotate: animationState === 'story-complete' ? 360 : 0,
                    }}
                    transition={{ duration: 0.3, delay: 0.2 }}
                  >
                    <CheckCircle className="w-3 h-3 text-success-600" />
                  </motion.div>
                </div>
                <div className="p-2 overflow-hidden">
                  <div className="space-y-1 overflow-hidden">
                    {[
                      { name: "findPetById", desc: "GET /pet/{id}", color: "blue" },
                      { name: "deletePet", desc: "DELETE /pet/{id}", color: "red" },
                      { name: "addPet", desc: "POST /pet", color: "green" },
                    ].map((tool, index) => (
                      <motion.div
                        key={tool.name}
                        className="flex items-center gap-2 p-1 rounded-md"
                        initial={{ opacity: 0, x: 10 }}
                        animate={{ 
                          opacity: animationState === 'story-complete' ? 1 : 0,
                          x: animationState === 'story-complete' ? 0 : 10,
                        }}
                        transition={{ duration: 0.3, delay: 0.4 + index * 0.1 }}
                      >
                        <div className={`w-1 h-1 rounded-full ${
                          tool.color === "blue" ? "bg-brand-blue-500" :
                          tool.color === "green" ? "bg-brand-green-500" :
                          tool.color === "red" ? "bg-brand-red-500" : ""
                        }`} />
                        <div className="flex-1">
                          <div className="font-mono text-[8px] text-neutral-900">{tool.name}</div>
                          <div className="text-[7px] text-neutral-500">{tool.desc}</div>
                        </div>
                      </motion.div>
                    ))}
                  </div>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </div>
    </div>
  );
}

export default function APIToolsSection() {
  const [, setHoveredFeature] = useState<number>(-1);

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
        <AnimatedAPITransform />
      </div>
    </div>
  );
}

export { AnimatedAPITransform };