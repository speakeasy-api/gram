import { cn } from "@/lib/utils";
import { Link, useLocation } from "react-router";
import { useSlugs } from "@/contexts/Sdk";

export function ObserveTabNav({ base }: { base: "insights" | "logs" }) {
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  const baseSlug = `/${orgSlug}/projects/${projectSlug}/${base}`;
  const tabs = [
    { label: "Hooks", href: `${baseSlug}/hooks` },
    { label: "MCP Servers", href: `${baseSlug}/mcp` },
    { label: "Agents", href: `${baseSlug}/agents` },
  ];

  return (
    <div className="border-border flex h-auto w-full items-end border-b px-8">
      {tabs.map((tab) => {
        const isActive = location.pathname === tab.href;
        return (
          <Link
            key={tab.href}
            to={tab.href}
            className={cn(
              "relative flex-none px-4 py-3 text-sm font-medium no-underline transition-colors",
              "after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5",
              isActive
                ? "text-foreground after:bg-primary"
                : "text-muted-foreground hover:text-foreground bg-transparent after:bg-transparent",
            )}
          >
            {tab.label}
          </Link>
        );
      })}
    </div>
  );
}
