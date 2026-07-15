import { ArrowRight, ExternalLink, LifeBuoy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";

interface OnboardingHeaderProps {
  onLeave?: () => void;
}

export function OnboardingHeader({
  onLeave,
}: OnboardingHeaderProps): JSX.Element {
  const handleGetSupport = () => {
    window.Pylon?.("show");
  };

  return (
    <header className="border-border bg-background w-full border-b">
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between py-4">
        <div className="flex items-center gap-3">
          <GramLogo variant="horizontal" className="w-32" />
          <div className="bg-border h-5 w-px" />
          <Type small className="font-medium">
            Setup organization
          </Type>
        </div>
        <div className="flex items-center gap-2">
          <Button
            asChild
            variant="tertiary"
            size="sm"
            className="text-muted-foreground hover:text-foreground"
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
            onClick={handleGetSupport}
            className="text-muted-foreground hover:text-foreground"
          >
            <Button.LeftIcon>
              <LifeBuoy className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Get support</Button.Text>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onLeave}
            className="text-muted-foreground hover:text-foreground"
          >
            <Button.Text>Go to dashboard</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </div>
      </div>
    </header>
  );
}
