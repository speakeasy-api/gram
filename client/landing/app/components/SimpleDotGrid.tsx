"use client";

import { motion } from "framer-motion";

export default function SimpleDotGrid() {
  const rows = 12;
  const cols = 12;
  
  return (
    <div className="relative w-full h-full overflow-hidden">
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="grid grid-cols-12 gap-3 p-8">
          {Array.from({ length: rows * cols }).map((_, index) => {
            const row = Math.floor(index / cols);
            const col = index % cols;
            const delay = (row + col) * 0.02;
            
            // Create a simple circular pattern
            const centerRow = rows / 2;
            const centerCol = cols / 2;
            const distance = Math.sqrt(
              Math.pow(row - centerRow, 2) + Math.pow(col - centerCol, 2)
            );
            const maxDistance = Math.sqrt(
              Math.pow(centerRow, 2) + Math.pow(centerCol, 2)
            );
            const scale = 1 - (distance / maxDistance) * 0.5;
            
            return (
              <motion.div
                key={index}
                className="w-2 h-2 rounded-full bg-neutral-300"
                initial={{ opacity: 0, scale: 0 }}
                animate={{ 
                  opacity: 0.3 + (scale * 0.4),
                  scale: scale
                }}
                transition={{
                  duration: 0.6,
                  delay: delay,
                  ease: "easeOut"
                }}
                whileHover={{
                  scale: scale * 1.5,
                  opacity: 0.8,
                  backgroundColor: "rgb(var(--color-brand-blue-500))"
                }}
              />
            );
          })}
        </div>
      </div>
      
      {/* Center focal point */}
      <motion.div
        className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2"
        initial={{ scale: 0, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ duration: 0.8, delay: 0.5 }}
      >
        <div className="w-24 h-24 rounded-full bg-gradient-primary opacity-20 blur-xl" />
      </motion.div>
    </div>
  );
}