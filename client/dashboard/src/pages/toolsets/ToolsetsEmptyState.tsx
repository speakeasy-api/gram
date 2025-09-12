import { EmptyState } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { cn } from "@/lib/utils";
import { motion } from "motion/react";
import { useState } from "react";

export function ToolsetsEmptyState({
  onCreateToolset,
}: {
  onCreateToolset: () => void;
}) {
  const cta = (
    <Button size="sm" onClick={onCreateToolset}>
      CREATE A TOOLSET
    </Button>
  );

  return (
    <EmptyState
      heading="No toolsets yet"
      description="Toolsets are a way to organize your tools into groupings of appropriate size for an LLM to use effectively."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}

export function ToolsetsGraphic({ className }: { className?: string }) {
  const [isHovered, setIsHovered] = useState(false);

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
    <div
      className={cn("w-full max-w-sm", className)}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      <div className="space-y-2">
        {toolsets.map((toolset, index) => (
          <motion.div
            key={toolset.id}
            className="bg-white rounded-xl p-4 border border-neutral-200"
            initial={{ opacity: 1, y: 0 }}
            animate={{
              opacity: 1,
              y: 0,
              scale: isHovered ? 1.02 : 1,
            }}
            transition={{
              duration: 0.5,
              delay: isHovered ? index * 0.1 : 0,
              ease: [0.21, 0.47, 0.32, 0.98],
            }}
            whileHover={{
              scale: 1.02,
              boxShadow: "0px 8px 16px rgba(0,0,0,0.08)",
            }}
          >
            <div className="flex items-center gap-3 mb-3">
              <motion.div
                className="w-8 h-8 rounded-lg bg-neutral-100 flex items-center justify-center"
                animate={{
                  backgroundColor: isHovered ? "#f3f4f6" : "#f5f5f5",
                }}
                transition={{ duration: 0.3 }}
              >
                <span className="text-xs font-bold text-neutral-700">
                  {toolset.icon}
                </span>
              </motion.div>
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
                  initial={{ opacity: 1, scale: 1 }}
                  animate={{
                    opacity: 1,
                    scale: isHovered ? 1.05 : 1,
                  }}
                  transition={{
                    delay: isHovered ? index * 0.1 + 0.2 + toolIndex * 0.05 : 0,
                    duration: 0.3,
                  }}
                  whileHover={{
                    scale: 1.05,
                    backgroundColor:
                      tool.type === "internal" ? "#e5e5e5" : "#f9f9f9",
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
