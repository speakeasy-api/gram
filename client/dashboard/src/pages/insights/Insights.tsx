import { Link, Outlet, useLocation } from "react-router";
import { InsightsHooksContent } from "@/components/observe/InsightsHooksContent";
import { MCPInsights } from "@/components/observe/MCPInsights";
import { RequireScope } from "@/components/require-scope";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";

function InsightsTabNav() {
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  const base = `/${orgSlug}/${projectSlug}/insights`;
  const tabs = [
    { label: "Hooks", href: `${base}/hooks` },
    { label: "MCP Servers", href: `${base}/mcp` },
    { label: "Agents", href: `${base}/agents` },
  ];

  return (
    <div className="border-border flex h-auto w-full items-end gap-8 border-b bg-transparent px-8 pt-2">
      {tabs.map((tab) => {
        const isActive = location.pathname === tab.href;
        return (
          <Link
            key={tab.href}
            to={tab.href}
            className={cn(
              "relative flex-none pb-3 text-sm font-medium no-underline transition-colors",
              "after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5",
              isActive
                ? "text-foreground after:bg-primary"
                : "text-muted-foreground hover:text-foreground after:bg-transparent",
            )}
          >
            {tab.label}
          </Link>
        );
      })}
    </div>
  );
}

export function InsightsRoot() {
  return (
    <div className="flex h-full flex-col">
      <InsightsTabNav />
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  );
}

export function InsightsHooksPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsHooksContent />
    </RequireScope>
  );
}

export function InsightsMCPPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <MCPInsights />
    </RequireScope>
  );
}
