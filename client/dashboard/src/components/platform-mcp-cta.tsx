import {
  type PlatformMcpCtaSurface,
  usePlatformMcpCta,
  usePlatformMcpCtaImpression,
} from "@/hooks/usePlatformMcpCta";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { PlugZap, X } from "lucide-react";
import { Link } from "react-router";

export function PlatformMcpPromotion({
  surface,
  projectSlug,
  className,
}: {
  surface: PlatformMcpCtaSurface;
  projectSlug?: string;
  className?: string;
}): JSX.Element | null {
  const cta = usePlatformMcpCta({ surface, projectSlug });
  const impressionRef = usePlatformMcpCtaImpression(
    cta.visible,
    cta.recordImpression,
  );

  if (!cta.visible) return null;

  return (
    <div
      ref={impressionRef}
      className={className ?? "relative overflow-hidden border bg-card p-4"}
    >
      <span
        aria-hidden="true"
        className="bg-gradient-primary absolute inset-x-0 top-0 h-px"
      />
      <button
        type="button"
        aria-label="Dismiss Platform MCP recommendation"
        title="Dismiss Platform MCP recommendation"
        onClick={cta.dismiss}
        className="text-muted-foreground hover:text-foreground absolute top-2 right-2 p-1 transition-colors"
      >
        <X className="size-4" />
      </button>
      <div className="flex gap-3 pr-6">
        <div className="bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center">
          <PlugZap className="size-4" />
        </div>
        <div className="min-w-0">
          <Text variant="subheading">Bring setup into your agent</Text>
          <Text small muted className="mt-1 max-w-2xl">
            Connect Platform MCP to choose a reviewed MCP catalogue server and
            add it to this project&apos;s Default plugin.
          </Text>
          <Link
            to={cta.href}
            onClick={cta.recordSelected}
            className="mt-3 inline-flex hover:no-underline"
          >
            <Button size="sm" variant="secondary">
              <Button.Text>{cta.label}</Button.Text>
            </Button>
          </Link>
        </div>
      </div>
    </div>
  );
}
