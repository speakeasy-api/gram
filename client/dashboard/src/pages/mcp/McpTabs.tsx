import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

export type McpTab = "servers" | "sources" | "deployments";

/**
 * The MCP index's three views.
 *
 * Servers are the inventory; sources are what they can be built from, and
 * deployments are the versions those sources arrive in. They are separate
 * routes rather than one page's state — each is linked to from the CLI, from
 * a source's own page, and from each other — so the strip is rendered by each
 * page rather than owning them.
 */
export function McpTabs({ active }: { active: McpTab }): JSX.Element {
  const routes = useRoutes();

  const tabs: { value: McpTab; label: string; href: string }[] = [
    { value: "servers", label: "MCP Servers", href: routes.mcp.href() },
    { value: "sources", label: "Sources", href: routes.mcp.sources.href() },
    {
      value: "deployments",
      label: "Deployments",
      href: routes.mcp.deployments.href(),
    },
  ];

  return (
    // The page header already closes with its own bottom margin; the strip
    // pulls up into it so the tabs sit with the title rather than floating
    // halfway to the content they switch.
    <Tabs value={active} className="-mt-6 mb-4 w-full">
      <div className="border-b">
        <PageTabsList className="h-auto gap-6 bg-transparent p-0">
          {tabs.map((tab) => (
            <PageTabsTrigger key={tab.value} value={tab.value} asChild>
              <Link to={tab.href}>{tab.label}</Link>
            </PageTabsTrigger>
          ))}
        </PageTabsList>
      </div>
    </Tabs>
  );
}
