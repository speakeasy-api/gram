import { useState, useEffect, useCallback } from "react";
import { motion, AnimatePresence, useMotionValue } from "motion/react";
import { GridOverlay } from "./grid-overlay";
import { cn } from "@/lib/utils";
import { RefreshCcw } from "lucide-react";

// Gram TypeScript function example
const GRAM_FUNCTION = `import { Gram } from "@gram-ai/functions";
import { createClient } from "@supabase/supabase-js";
import * as z from "zod/mini";

const gram = new Gram({
  envSchema: {
    SUPABASE_URL: z.string(),
    SUPABASE_ANON_KEY: z.string()
  },
}).tool({
  name: "top_cities_by_property_sales",
  description: "Get top N UK cities by sales",
  inputSchema: {
    count: z._default(z.number(), 10)
  },
  async execute(ctx, input) {
    const supabase = createClient(
      ctx.env.SUPABASE_URL,
      ctx.env.SUPABASE_ANON_KEY
    );

    const { data, error } = await supabase
      .from("land_registry_price_paid_uk")
      .select(\`
        city::text,
        count(),
        price.avg(),
        price.max()
      \`)
      .eq("record_status", "A")
      .not("city", "is", null)
      .neq("city", "")
      .order("count", { ascending: false })
      .limit(input.count);

    if (error != null) {
      throw new Error(
        \`Query failed: \${error.message}\`
      );
    }

    return ctx.json(data);
  },
});

export default gram;`;

type Tool = {
  id: string;
  name: string;
  method: "GET" | "POST" | "PUT" | "DELETE";
  path: string;
};

const GENERATED_TOOLS: Tool[] = [
  {
    id: "1",
    name: "getCitiesByPropertySales",
    method: "GET",
    path: "/cities/by-sales",
  },
  {
    id: "2",
    name: "getPropertyPrices",
    method: "GET",
    path: "/properties/prices",
  },
  {
    id: "3",
    name: "searchProperties",
    method: "GET",
    path: "/properties/search",
  },
  { id: "4", name: "getPropertyById", method: "GET", path: "/properties/{id}" },
  {
    id: "5",
    name: "getCityStats",
    method: "GET",
    path: "/cities/{city}/stats",
  },
];

const getMethodColor = (method: string) => {
  switch (method) {
    case "GET":
      return "text-blue-400";
    case "POST":
      return "text-emerald-400";
    case "PUT":
      return "text-amber-400";
    case "DELETE":
      return "text-red-400";
    default:
      return "text-zinc-400";
  }
};

export function JourneyDemo() {
  const [visibleTools, setVisibleTools] = useState<Tool[]>([]);
  const [hasMoved, setHasMoved] = useState(false);
  const [hasChangedFocus, setHasChangedFocus] = useState(false);
  const [animationComplete, setAnimationComplete] = useState(false);
  const [focusedWindow, setFocusedWindow] = useState<"spec" | "tools">("spec");
  const [animationKey, setAnimationKey] = useState(0);

  const specX = useMotionValue(-150);
  const specY = useMotionValue(50);
  const toolsX = useMotionValue(150);
  const toolsY = useMotionValue(-50);

  // Animate tools appearing one by one
  useEffect(() => {
    const timers: NodeJS.Timeout[] = [];

    // Initial delay before tools start appearing
    const initialDelay = 1000;

    GENERATED_TOOLS.forEach((tool, index) => {
      const timer = setTimeout(
        () => {
          setVisibleTools((prev) => [...prev, tool]);

          // Mark complete after last tool appears
          if (index === GENERATED_TOOLS.length - 1) {
            const completeTimer = setTimeout(() => {
              setAnimationComplete(true);
            }, 500);
            timers.push(completeTimer);
          }
        },
        initialDelay + index * 600,
      );

      timers.push(timer);
    });

    return () => timers.forEach(clearTimeout);
  }, [animationKey]);

  // Track if windows have been moved
  useEffect(() => {
    const unsubscribeSpecX = specX.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - -150) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeSpecY = specY.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - 50) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeToolsX = toolsX.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - 150) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeToolsY = toolsY.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - -50) > 5) {
        setHasMoved(true);
      }
    });

    return () => {
      unsubscribeSpecX();
      unsubscribeSpecY();
      unsubscribeToolsX();
      unsubscribeToolsY();
    };
  }, [hasMoved, specX, specY, toolsX, toolsY]);

  const handleReset = useCallback(() => {
    specX.set(-150);
    specY.set(50);
    toolsX.set(150);
    toolsY.set(-50);
    setHasMoved(false);
    setHasChangedFocus(false);
    setAnimationComplete(false);
    setFocusedWindow("spec");
    setVisibleTools([]);
    setAnimationKey((prev) => prev + 1);
  }, [specX, specY, toolsX, toolsY]);

  const handleWindowClick = useCallback((window: "spec" | "tools") => {
    setFocusedWindow(window);
    setHasChangedFocus(true);
  }, []);

  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen bg-black relative border-gradient-primary border-8 border-t-0 border-x-0 p-8">
      <GridOverlay />
      <div className="flex-1 flex items-center justify-center w-full relative overflow-hidden scale-[0.65] lg:scale-75 xl:scale-90">
        {/* OpenAPI Spec Window - left side */}
        <motion.div
          drag
          dragMomentum={false}
          dragElastic={0}
          dragConstraints={{
            left: -400,
            right: 400,
            top: -400,
            bottom: 400,
          }}
          className={cn(
            "absolute w-[450px] bg-zinc-900 border border-zinc-700 rounded-lg overflow-hidden cursor-pointer",
            focusedWindow === "spec" ? "z-20 shadow-xl" : "z-10 shadow-sm",
          )}
          style={{ x: specX, y: specY }}
          onClick={() => handleWindowClick("spec")}
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, ease: "easeOut" }}
        >
          {/* Window header */}
          <div className="bg-zinc-800 border-b border-zinc-700 px-4 py-2 flex items-center justify-between cursor-grab active:cursor-grabbing">
            <div className="flex gap-1.5">
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "spec" ? "bg-red-500/80" : "bg-zinc-600/50",
                )}
              />
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "spec"
                    ? "bg-yellow-500/80"
                    : "bg-zinc-600/50",
                )}
              />
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "spec"
                    ? "bg-green-500/80"
                    : "bg-zinc-600/50",
                )}
              />
            </div>
            <span className="text-sm text-zinc-400 absolute left-1/2 -translate-x-1/2 font-mono">
              gram.ts
            </span>
            <div className="w-[42px]" />
          </div>

          {/* Gram function content */}
          <div className="p-4 h-[350px] overflow-y-auto">
            <pre className="font-mono text-xs text-zinc-300 whitespace-pre">
              {GRAM_FUNCTION}
            </pre>
          </div>
        </motion.div>

        {/* Generated Tools Window - right side */}
        <motion.div
          drag
          dragMomentum={false}
          dragElastic={0}
          dragConstraints={{
            left: -400,
            right: 400,
            top: -400,
            bottom: 400,
          }}
          className={cn(
            "absolute w-[450px] bg-zinc-900 border border-zinc-700 rounded-lg overflow-hidden cursor-pointer",
            focusedWindow === "tools" ? "z-20 shadow-xl" : "z-10 shadow-sm",
          )}
          style={{ x: toolsX, y: toolsY }}
          onClick={() => handleWindowClick("tools")}
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, ease: "easeOut", delay: 0.2 }}
        >
          {/* Window header */}
          <div className="bg-zinc-800 border-b border-zinc-700 px-4 py-2 flex items-center justify-between cursor-grab active:cursor-grabbing">
            <div className="flex gap-1.5">
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "tools"
                    ? "bg-red-500/80"
                    : "bg-zinc-600/50",
                )}
              />
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "tools"
                    ? "bg-yellow-500/80"
                    : "bg-zinc-600/50",
                )}
              />
              <div
                className={cn(
                  "w-3 h-3 rounded-full",
                  focusedWindow === "tools"
                    ? "bg-green-500/80"
                    : "bg-zinc-600/50",
                )}
              />
            </div>
            <span className="text-sm text-zinc-400 absolute left-1/2 -translate-x-1/2 font-mono">
              Deployed Tools
            </span>
            <div className="w-[42px]" />
          </div>

          {/* Tools content */}
          <div className="p-4 h-[350px] overflow-y-auto space-y-2">
            {visibleTools.length === 0 && (
              <div className="h-full flex items-center justify-center">
                <span className="text-zinc-500 text-sm">
                  Generating tools...
                </span>
              </div>
            )}

            <AnimatePresence>
              {visibleTools.map((tool) => (
                <motion.div
                  key={tool.id}
                  initial={{ opacity: 0, x: -20, scale: 0.9 }}
                  animate={{ opacity: 1, x: 0, scale: 1 }}
                  transition={{
                    type: "spring",
                    stiffness: 200,
                    damping: 20,
                    delay: 0,
                  }}
                  className="border border-zinc-700 rounded-md p-3 bg-zinc-800/50 font-mono text-sm"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-white">{tool.name}()</span>
                    <span
                      className={cn(
                        "text-xs font-semibold",
                        getMethodColor(tool.method),
                      )}
                    >
                      {tool.method}
                    </span>
                  </div>
                  <div className="text-zinc-400 text-xs mt-1">{tool.path}</div>
                </motion.div>
              ))}
            </AnimatePresence>
          </div>
        </motion.div>

        {/* Reset button */}
        <AnimatePresence>
          {(animationComplete || hasMoved || hasChangedFocus) && (
            <motion.button
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 10 }}
              transition={{ duration: 0.2 }}
              onClick={handleReset}
              className="absolute bottom-8 right-8 z-50 flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 border border-zinc-600 rounded-lg text-sm text-zinc-300 transition-colors"
            >
              <RefreshCcw className="w-4 h-4" />
              Reset
            </motion.button>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}
