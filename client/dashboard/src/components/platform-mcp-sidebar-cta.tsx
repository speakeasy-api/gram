import { ArrowRight, PlugZap, X } from "lucide-react";
import {
  usePlatformMcpCta,
  usePlatformMcpCtaImpression,
} from "@/hooks/usePlatformMcpCta";

import { Link } from "react-router";
import { useSlugs } from "@/contexts/Sdk";

export function PlatformMcpSidebarCta(): JSX.Element | null {
  const { projectSlug } = useSlugs();
  const { dismiss, href, label, recordImpression, recordSelected, visible } =
    usePlatformMcpCta({ surface: "sidebar_footer", projectSlug });
  const impressionRef = usePlatformMcpCtaImpression(visible, recordImpression);

  if (!visible) return null;

  return (
    <div
      ref={impressionRef}
      className="group border-border/60 bg-card relative overflow-hidden border p-3 group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:bg-transparent group-data-[collapsible=icon]:p-0"
    >
      <Link
        to={href}
        onClick={recordSelected}
        aria-label={`Platform MCP: ${label}`}
        className="absolute inset-0 z-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset group-data-[collapsible=icon]:hidden"
      />
      <span
        aria-hidden="true"
        className="bg-gradient-primary absolute inset-x-0 top-0 z-10 h-px pointer-events-none group-data-[collapsible=icon]:hidden"
      />
      <div className="group-data-[collapsible=icon]:hidden">
        <button
          type="button"
          aria-label="Dismiss Platform MCP recommendation"
          title="Dismiss Platform MCP recommendation"
          onClick={dismiss}
          className="text-muted-foreground hover:text-foreground hover:bg-background/80 absolute top-2 right-2 z-20 p-1 transition-colors"
        >
          <X className="size-3.5" />
        </button>
        <div className="relative z-10 flex gap-2.5 pointer-events-none">
          <div className="bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center">
            <PlugZap className="size-4" />
          </div>
          <div className="min-w-0 pr-5">
            <p className="text-foreground text-sm font-medium">Platform MCP</p>
            <p className="text-muted-foreground mt-0.5 text-xs leading-relaxed">
              Add a reviewed MCP server to your project&apos;s Default plugin.
            </p>
          </div>
        </div>
        <span className="text-foreground relative z-10 mt-3 flex items-center gap-1 text-sm font-medium pointer-events-none">
          {label}
          <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
        </span>
      </div>
      <Link
        to={href}
        onClick={recordSelected}
        aria-label={label}
        title={label}
        className="hidden text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:justify-center"
      >
        <PlugZap className="size-4" />
      </Link>
    </div>
  );
}
