import { cn } from "@/lib/utils";
import { Link, Outlet, useLocation } from "react-router";
import { useSlugs } from "@/contexts/Sdk";

function LogsTabNav() {
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  const base = `/${orgSlug}/${projectSlug}/logs`;
  const tabs = [
    { label: "MCP", href: `${base}/mcp` },
    { label: "Hooks", href: `${base}/hooks` },
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

export function LogsRoot() {
  return (
    <div className="flex h-full flex-col">
      <LogsTabNav />
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  );
}
