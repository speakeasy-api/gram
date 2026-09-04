import type { ReactNode } from "react";
import { ArrowRight, ExternalLink, LifeBuoy } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { GramLogo } from "@/components/gram-logo";
import { showPylonChat } from "@/lib/pylon";

interface OnboardingHeaderProps {
  onLeave?: () => void;
  children?: ReactNode;
}

export function OnboardingHeader({
  onLeave,
  children,
}: OnboardingHeaderProps): JSX.Element {
  return (
    <header className="border-border bg-background w-full border-b">
      <div className="flex w-full items-center justify-between px-4 py-4">
        <GramLogo variant="horizontal" className="w-32" />
        <div className="flex items-center gap-2">
          <Button
            asChild
            variant="tertiary"
            size="sm"
            className="text-muted-foreground hover:text-foreground hidden gap-1.5 lg:inline-flex"
          >
            <a
              href="https://www.speakeasy.com/docs/mcp"
              target="_blank"
              rel="noopener noreferrer"
            >
              Docs
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={showPylonChat}
            className="text-muted-foreground hover:text-foreground hidden gap-1.5 lg:inline-flex"
          >
            <LifeBuoy className="h-4 w-4" />
            Get support
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onLeave}
            aria-label="Go to dashboard"
            className="text-muted-foreground hover:text-foreground inline-flex gap-1.5"
          >
            <span className="hidden lg:inline">Go to dashboard</span>
            <ArrowRight className="h-4 w-4" />
          </Button>
          {children}
        </div>
      </div>
    </header>
  );
}
