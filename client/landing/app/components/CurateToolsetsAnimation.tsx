"use client";

import { useRef } from "react";
import { motion, useInView } from "framer-motion";

export default function CurateToolsetsAnimation() {
  const ref = useRef(null);
  const isInView = useInView(ref, { once: true, amount: 0.5 });

  const toolsets = [
    {
      id: "engineering",
      name: "Engineering",
      icon: "E",
      color: "neutral",
      tools: [
        { name: "your-api", type: "internal", label: "/deploy" },
        { name: "github", type: "external", label: "GitHub" },
        { name: "datadog", type: "external", label: "Datadog" },
        { name: "your-api-2", type: "internal", label: "/logs" },
      ],
    },
    {
      id: "sales",
      name: "Sales",
      icon: "S",
      color: "neutral",
      tools: [
        { name: "your-api-3", type: "internal", label: "/customers" },
        { name: "salesforce", type: "external", label: "Salesforce" },
        { name: "hubspot", type: "external", label: "HubSpot" },
        { name: "your-api-4", type: "internal", label: "/analytics" },
      ],
    },
    {
      id: "marketing",
      name: "Marketing",
      icon: "M",
      color: "neutral",
      tools: [
        { name: "your-api-5", type: "internal", label: "/campaigns" },
        { name: "mailchimp", type: "external", label: "Mailchimp" },
        { name: "analytics", type: "external", label: "Analytics" },
        { name: "your-api-6", type: "internal", label: "/content" },
      ],
    },
  ];

  return (
    <div ref={ref} className="w-full max-w-sm">
      <div className="space-y-2">
        {toolsets.map((toolset, index) => (
          <motion.div
            key={toolset.id}
            className="bg-white rounded-xl p-4 border border-neutral-200"
            initial={{ opacity: 0, y: 20 }}
            animate={{
              opacity: isInView ? 1 : 0,
              y: isInView ? 0 : 20,
            }}
            transition={{
              duration: 0.5,
              delay: index * 0.1,
              ease: [0.21, 0.47, 0.32, 0.98],
            }}
          >
            <div className="flex items-center gap-3 mb-3">
              <div className="w-8 h-8 rounded-lg bg-neutral-100 flex items-center justify-center">
                <span className="text-xs font-bold text-neutral-700">
                  {toolset.icon}
                </span>
              </div>
              <div>
                <div className="text-sm font-medium text-neutral-900">
                  {toolset.name}
                </div>
                <div className="text-xs text-neutral-500">
                  {toolset.tools.filter((t) => t.type === "internal").length}{" "}
                  internal,{" "}
                  {toolset.tools.filter((t) => t.type === "external").length}{" "}
                  external
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-2">
              {toolset.tools.map((tool, toolIndex) => (
                <motion.div
                  key={tool.name}
                  className={`px-2.5 py-1 rounded-md text-xs font-medium ${
                    tool.type === "internal"
                      ? "bg-neutral-100 text-neutral-900 ring-1 ring-inset ring-neutral-900/10"
                      : "bg-neutral-50 text-neutral-600"
                  }`}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: isInView ? 1 : 0 }}
                  transition={{
                    delay: index * 0.1 + 0.2 + toolIndex * 0.05,
                    duration: 0.3,
                  }}
                >
                  {tool.label}
                </motion.div>
              ))}
            </div>
          </motion.div>
        ))}
      </div>
    </div>
  );
}