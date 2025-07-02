"use client";

import { useRef } from "react";
import { motion, useInView } from "framer-motion";
import { Users, Layers } from "lucide-react";

export default function GramEcosystemAnimation() {
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