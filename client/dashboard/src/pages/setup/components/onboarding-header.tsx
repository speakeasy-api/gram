import { RotateCcw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Logo } from "@speakeasy-api/moonshine";

interface OnboardingHeaderProps {
  onRestart?: () => void;
  onLeave?: () => void;
}

export function OnboardingHeader({
  onRestart,
  onLeave,
}: OnboardingHeaderProps) {
  return (
    <header className="border-border bg-background w-full border-b">
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between py-4">
        <div className="flex items-center gap-3">
          <Logo variant="wordmark" className="w-32" />
          <div className="bg-border h-4 w-px" />
          <span className="text-foreground text-sm font-medium">
            Set up workspace
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={onRestart}
            className="text-muted-foreground hover:text-foreground gap-1.5"
          >
            <RotateCcw className="h-4 w-4" />
            Restart
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={onLeave}
            className="text-muted-foreground hover:text-foreground gap-1.5"
          >
            <X className="h-4 w-4" />
            Leave
          </Button>
        </div>
      </div>
    </header>
  );
}
