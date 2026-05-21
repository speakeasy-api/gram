import { cn } from "@/lib/utils";
import { Link, useLocation } from "react-router";
import { useSlugs } from "@/contexts/Sdk";
import { RequireScope } from "@/components/require-scope";
import type { Scope } from "@/hooks/useRBAC";
import {
  ReleaseStage,
  ReleaseStageBadge,
} from "@/components/release-stage-badge";

type Tab = {
  label: string;
  href: string;
  stage?: ReleaseStage;
  scope?: Scope | Scope[];
};

export function ObserveTabNav({ base }: { base: "insights" | "logs" }) {
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();

  const baseSlug = `/${orgSlug}/projects/${projectSlug}/${base}`;
  const tabs: Tab[] = [
    { label: "Tools", href: `${baseSlug}/tools` },
    { label: "MCP Servers", href: `${baseSlug}/mcp` },
    ...(base === "logs"
      ? ([
          {
            label: "Risk Events",
            href: `${baseSlug}/risk-events`,
            stage: "beta",
            scope: "org:admin",
          },
          { label: "Agents", href: `${baseSlug}/agents` },
        ] satisfies Tab[])
      : []),
    ...(base === "insights"
      ? ([
          {
            label: "Employees",
            href: `${baseSlug}/employees`,
            stage: "preview",
          },
          { label: "Costs", href: `${baseSlug}/costs`, stage: "preview" },
        ] satisfies Tab[])
      : []),
  ];

  return (
    <div className="border-border flex h-auto w-full items-end border-b px-8">
      {tabs.map((tab) => {
        const isActive =
          location.pathname === tab.href ||
          location.pathname.startsWith(tab.href + "/");
        const link = (
          <Link
            key={tab.href}
            to={tab.href}
            className={cn(
              "relative flex-none px-4 py-3 text-sm font-medium no-underline transition-colors",
              "after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5",
              "inline-flex items-center gap-2",
              isActive
                ? "text-foreground after:bg-primary"
                : "text-muted-foreground hover:text-foreground bg-transparent after:bg-transparent",
            )}
          >
            {tab.label}
            {tab.stage && (
              <ReleaseStageBadge stage={tab.stage} size="xs" noTooltip />
            )}
          </Link>
        );

        if (tab.scope) {
          return (
            <RequireScope key={tab.href} scope={tab.scope} level="section">
              {link}
            </RequireScope>
          );
        }

        return link;
      })}
    </div>
  );
}
