import { ArrowRight, ExternalLink, LifeBuoy } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { GramLogo } from "@/components/gram-logo";
import { showPylonChat } from "@/lib/pylon";

interface OnboardingHeaderProps {
  onLeave?: () => void;
}

export function OnboardingHeader({
  onLeave,
}: OnboardingHeaderProps): JSX.Element {
  return (
    <header className="border-border bg-background w-full border-b">
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between py-4">
        <div className="flex items-center gap-3">
          <GramLogo variant="horizontal" className="w-32" />
          <div className="bg-border h-5 w-px" />
          <span className="text-foreground text-sm font-medium">
            Setup organization
          </span>
        </div>
        <div className="flex items-center gap-2">
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
            <LifeBuoy className="h-4 w-4" />
            Get support
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onLeave}
            className="text-muted-foreground hover:text-foreground gap-1.5"
          >
            Go to dashboard
            <ArrowRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </header>
  );
}
