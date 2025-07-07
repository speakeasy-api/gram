"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { useBentoItemState } from "./BentoGrid";

export default function StackedMetricCards() {
  const { isHovered } = useBentoItemState();
  const [hoveredCard, setHoveredCard] = useState<number | null>(null);

  const cards = [
    {
      id: 1,
      title: "Requests/hour",
      value: "18.2k",
      change: "↑ 47%",
      changeColor: "text-success-600",
      position: { x: "0%", y: "-10%" },
      rotate: -2,
      chart: (
        <div className="h-4 flex items-end gap-0.5 w-full">
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
      position: { x: "45%", y: "5%" },
      rotate: 1.5,
      chart: (
        <div className="space-y-1 w-full">
          <div className="flex items-center gap-1">
            <span className="text-[6px] text-neutral-500 flex-shrink-0">
              p50
            </span>
            <div className="flex-1 h-1 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-400 rounded-full"
                style={{ width: "35%" }}
              />
            </div>
            <span className="text-[6px] text-neutral-700 font-mono flex-shrink-0">
              8ms
            </span>
          </div>
          <div className="flex items-center gap-1">
            <span className="text-[6px] text-neutral-500 flex-shrink-0">
              p99
            </span>
            <div className="flex-1 h-1 bg-neutral-100 rounded-full overflow-hidden min-w-0">
              <div
                className="h-full bg-info-500 rounded-full"
                style={{ width: "60%" }}
              />
            </div>
            <span className="text-[6px] text-neutral-700 font-mono flex-shrink-0">
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
      position: { x: "20%", y: "35%" },
      rotate: -1,
      chart: (
        <>
          <div className="grid grid-cols-7 gap-0.5 w-full">
            {Array.from({ length: 21 }, (_, i) => (
              <div
                key={i}
                className={`aspect-square rounded-sm ${
                  i === 10 ? "bg-warning-400" : "bg-success-400"
                }`}
              />
            ))}
          </div>
          <p className="text-[6px] text-neutral-500 mt-0.5">Last 21 days</p>
        </>
      ),
    },
  ];

  return (
    <div className="relative w-full h-[160px] mx-auto flex items-center justify-center">
      <div className="relative w-[200px] h-[160px]">
      {cards.map((card, index) => {
        const isCardHovered = hoveredCard === card.id;
        const anyCardHovered = hoveredCard !== null;

        return (
          <motion.div
            key={card.id}
            className="absolute w-[45%] max-w-[140px] min-w-[120px]"
            initial={{
              left: card.position.x,
              top: card.position.y,
              scale: 0.8,
              opacity: 1,
              rotate: card.rotate,
            }}
            animate={{
              left: card.position.x,
              top: card.position.y,
              scale: isHovered
                ? isCardHovered
                  ? 1.05
                  : anyCardHovered
                  ? 0.95
                  : 1
                : 0.8,
              opacity: anyCardHovered && !isCardHovered
                ? 0.6
                : 1,
              filter:
                anyCardHovered && !isCardHovered && isHovered
                  ? "blur(2px)"
                  : "blur(0px)",
              rotate: isHovered
                ? isCardHovered
                  ? 0
                  : card.rotate
                : card.rotate,
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
                delay: isHovered ? index * 0.1 : 0,
              },
              top: {
                type: "spring",
                stiffness: 260,
                damping: 30,
                delay: isHovered ? index * 0.1 : 0,
              },
            }}
            style={{
              zIndex: isCardHovered ? 10 : 3 - index,
              transformOrigin: "center center",
            }}
            onMouseEnter={() => setHoveredCard(card.id)}
            onMouseLeave={() => setHoveredCard(null)}
          >
            <motion.div
              className="bg-background-pure rounded-lg border border-neutral-200 p-2 cursor-pointer"
              animate={{
                boxShadow: isCardHovered
                  ? "0 20px 40px -8px rgba(0,0,0,0.15), 0 8px 16px -4px rgba(0,0,0,0.08)"
                  : "0 4px 24px -4px rgba(0,0,0,0.08)",
              }}
              transition={{ duration: 0.3 }}
            >
              <div className="flex flex-col h-full">
                <div className="mb-1.5">
                  <p className="text-[8px] text-neutral-600 mb-0.5 truncate">
                    {card.title}
                  </p>
                  <div className="flex items-baseline gap-1">
                    <p className="text-sm font-mono text-neutral-900 font-light">
                      {card.value}
                    </p>
                    <span
                      className={`text-[7px] ${card.changeColor} flex-shrink-0`}
                    >
                      {card.change}
                    </span>
                  </div>
                </div>
                <div className="flex-1 flex flex-col justify-end min-h-[30px]">
                  {card.chart}
                </div>
              </div>
            </motion.div>
          </motion.div>
        );
      })}
      </div>
    </div>
  );
}
