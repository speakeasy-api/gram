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
    <header className="border-border bg-background w-full shrink-0 border-b px-4 sm:px-8">
      <div className="mx-auto flex w-full max-w-7xl flex-col items-start justify-between gap-4 py-4 lg:flex-row lg:items-center">
        <div className="flex items-center gap-3">
          <GramLogo variant="horizontal" className="w-32" />
          <div className="bg-border h-5 w-px" />
          <span className="text-foreground text-sm font-medium">
            Setup organization
          </span>
        </div>
        <div className="flex w-full flex-wrap items-center gap-2 lg:w-auto">
          {children}
          <Button
            asChild
            variant="tertiary"
            size="sm"
            className="text-muted-foreground hover:text-foreground gap-1.5"
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
            className="text-muted-foreground hover:text-foreground gap-1.5"
          >
            <Button.LeftIcon>
              <LifeBuoy />
            </Button.LeftIcon>
            <Button.Text>Get support</Button.Text>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onLeave}
            className="text-muted-foreground hover:text-foreground gap-1.5"
          >
            <Button.Text>Go to dashboard</Button.Text>
            <Button.RightIcon>
              <ArrowRight />
            </Button.RightIcon>
          </Button>
        </div>
      </div>
    </header>
  );
}
