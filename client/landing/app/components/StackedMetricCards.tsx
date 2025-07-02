"use client";

import { useRef, useState } from "react";
import { motion, useInView } from "framer-motion";

export default function StackedMetricCards() {
  const ref = useRef(null);
  const isInView = useInView(ref, {
    once: true,
    amount: 0.5,
  });
  const [hoveredCard, setHoveredCard] = useState<number | null>(null);

  const cards = [
    {
      id: 1,
      title: "Requests/hour",
      value: "18.2k",
      change: "↑ 47%",
      changeColor: "text-success-600",
      position: { x: "0%", y: "0%" },
      rotate: -2,
      chart: (
        <div className="h-8 flex items-end gap-0.5 w-full">
          {[40, 45, 52, 48, 65, 72, 88, 100].map((height, i) => (
            <div
              key={i}
              className={`flex-1 rounded-sm min-w-0 ${
                i >= 6 ? "bg-success-500" : "bg-neutral-200"
              }`}
              style={{ height: `${height}%` }}
            />
          ))}
        </div>
      ),
    },
    {
      id: 2,
      title: "Response time",
      value: "12ms",
      change: "p99",
      changeColor: "text-info-600",
      position: { x: "45%", y: "15%" },
      rotate: 1.5,
      chart: (
        <div className="space-y-1.5 w-full">
          <div className="flex items-center gap-1 sm:gap-2">
            <span className="text-[10px] text-neutral-500 flex-shrink-0">
              p50
            </span>
            <div className="flex-1 h-1.5 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-400 rounded-full"
                style={{ width: "35%" }}
              />
            </div>
            <span className="text-[10px] text-neutral-700 font-mono flex-shrink-0">
              8ms
            </span>
          </div>
          <div className="flex items-center gap-1 sm:gap-2">
            <span className="text-[10px] text-neutral-500 flex-shrink-0">
              p99
            </span>
            <div className="flex-1 h-1.5 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-500 rounded-full"
                style={{ width: "60%" }}
              />
            </div>
            <span className="text-[10px] text-neutral-700 font-mono flex-shrink-0">
              12ms
            </span>
          </div>
        </div>
      ),
    },
    {
      id: 3,
      title: "Uptime",
      value: "99.9%",
      change: "SLA",
      changeColor: "text-success-600",
      position: { x: "20%", y: "50%" },
      rotate: -1,
      chart: (
        <>
          <div className="grid grid-cols-7 gap-0.5 w-full">
            {Array.from({ length: 28 }, (_, i) => (
              <div
                key={i}
                className={`aspect-square rounded-sm ${
                  i === 14 ? "bg-warning-400" : "bg-success-400"
                }`}
              />
            ))}
          </div>
          <p className="text-[9px] text-neutral-500 mt-1.5">Last 28 days</p>
        </>
      ),
    },
  ];

  return (
    <div ref={ref} className="relative w-full h-[380px] mx-auto">
      {cards.map((card, index) => {
        const isHovered = hoveredCard === card.id;
        const anyHovered = hoveredCard !== null;

        return (
          <motion.div
            key={card.id}
            className="absolute w-[55%] max-w-[220px] min-w-[180px]"
            initial={{
              left: card.position.x,
              top: card.position.y,
              scale: 0.8,
              opacity: 0,
              rotate: card.rotate,
            }}
            animate={{
              left: card.position.x,
              top: card.position.y,
              scale: isInView
                ? isHovered
                  ? 1.05
                  : anyHovered
                  ? 0.95
                  : 1
                : 0.8,
              opacity: isInView ? (anyHovered && !isHovered ? 0.6 : 1) : 0,
              filter: anyHovered && !isHovered ? "blur(2px)" : "blur(0px)",
              rotate: isInView ? (isHovered ? 0 : card.rotate) : card.rotate,
            }}
            transition={{
              scale: {
                type: "spring",
                stiffness: 300,
                damping: 25,
              },
              opacity: {
                duration: 0.3,
              },
              filter: {
                duration: 0.3,
              },
              rotate: {
                type: "spring",
                stiffness: 200,
                damping: 20,
              },
              left: {
                type: "spring",
                stiffness: 260,
                damping: 30,
                delay: isInView ? index * 0.1 : 0,
              },
              top: {
                type: "spring",
                stiffness: 260,
                damping: 30,
                delay: isInView ? index * 0.1 : 0,
              },
            }}
            style={{
              zIndex: isHovered ? 10 : 3 - index,
              transformOrigin: "center center",
            }}
            onMouseEnter={() => setHoveredCard(card.id)}
            onMouseLeave={() => setHoveredCard(null)}
          >
            <motion.div
              className="bg-background-pure rounded-xl border border-neutral-200 p-3 sm:p-4 md:p-5 cursor-pointer"
              animate={{
                boxShadow: isHovered
                  ? "0 20px 40px -8px rgba(0,0,0,0.15), 0 8px 16px -4px rgba(0,0,0,0.08)"
                  : "0 4px 24px -4px rgba(0,0,0,0.08)",
              }}
              transition={{ duration: 0.3 }}
            >
              <div className="flex flex-col h-full">
                <div className="mb-3">
                  <p className="text-[11px] sm:text-xs text-neutral-600 mb-1 truncate">
                    {card.title}
                  </p>
                  <div className="flex items-baseline gap-2">
                    <p className="text-xl sm:text-2xl font-mono text-neutral-900 font-light">
                      {card.value}
                    </p>
                    <span
                      className={`text-[10px] sm:text-xs ${card.changeColor} flex-shrink-0`}
                    >
                      {card.change}
                    </span>
                  </div>
                </div>
                <div className="flex-1 flex flex-col justify-end min-h-[60px]">
                  {card.chart}
                </div>
              </div>
            </motion.div>
          </motion.div>
        );
      })}
    </div>
  );
}