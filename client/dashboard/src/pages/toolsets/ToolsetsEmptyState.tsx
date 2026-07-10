import { cn } from "@/lib/utils";
import { motion } from "motion/react";
import { useState } from "react";

export function ToolsetsGraphic({
  className,
}: {
  className?: string;
}): JSX.Element {
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
            className="border-border bg-card border p-4"
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
          >
            <div className="mb-3 flex items-center gap-3">
              <div className="bg-muted flex h-8 w-8 items-center justify-center">
                <span className="text-foreground text-xs font-bold">
                  {toolset.icon}
                </span>
              </div>
              <div>
                <div className="text-foreground text-sm font-medium">
                  {toolset.name}
                </div>
                <div className="text-muted-foreground text-xs">
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
                  className={cn(
                    "px-2.5 py-1 text-xs font-medium",
                    tool.type === "internal"
                      ? "bg-muted text-foreground ring-border ring-1 ring-inset"
                      : "bg-muted/50 text-muted-foreground",
                  )}
                  initial={{ opacity: 1, scale: 1 }}
                  animate={{
                    opacity: 1,
                    scale: isHovered ? 1.05 : 1,
                  }}
                  transition={{
                    delay: isHovered ? index * 0.1 + 0.2 + toolIndex * 0.05 : 0,
                    duration: 0.3,
                  }}
                  whileHover={{ scale: 1.05 }}
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
