import { EmptyState } from "@/components/page-layout";
import { AnimatePresence, motion } from "motion/react";

export function MCPEmptyState() {
  return (
    <EmptyState
      heading="No MCP servers yet"
      description="Gram generates MCP-ready tools from your OpenAPI documents. Get a hosted MCP server in seconds, not days."
      graphic={<MCPGraphic />}
    />
  );
}

export default function MCPGraphic() {
  const TOOL_COUNT = 17;

  return (
    <div className="w-full max-w-sm">
      <div className="relative min-h-[140px]">
        <AnimatePresence mode="wait">
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
                        <span className="font-mono text-success-700">12ms</span>{" "}
                        avg
                      </span>
                    </div>
                    <div className="flex items-center gap-1">
                      <div className="w-0.5 h-0.5 rounded-full bg-brand-blue-500" />
                      <span className="text-neutral-600">
                        <span className="font-mono text-brand-blue-700">
                          99.9%
                        </span>{" "}
                        uptime
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}
