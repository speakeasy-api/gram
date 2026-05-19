import type { ReactNode } from "react";
import { ArrowRight, ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";

interface StepContainerProps {
  icon: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
  onContinue?: () => void;
  onBack?: () => void;
  continueLabel?: string;
  showBack?: boolean;
  isLoading?: boolean;
  canContinue?: boolean;
}

export function StepContainer({
  icon,
  title,
  description,
  children,
  onContinue,
  onBack,
  continueLabel = "Continue",
  showBack = false,
  isLoading = false,
  canContinue = true,
}: StepContainerProps) {
  return (
    <div className="flex h-full flex-col">
      {/* Icon */}
      <div className="mb-6">{icon}</div>

      {/* Header */}
      <h1 className="text-foreground text-2xl font-semibold tracking-tight">
        {title}
      </h1>
      <p className="text-muted-foreground mt-2 text-sm">{description}</p>

      {/* Content */}
      <div className="mt-8 flex-1">{children}</div>

      {/* Divider */}
      <div className="bg-border mt-8 h-px" />

      {/* Actions */}
      <div className="mt-6 flex items-center justify-between">
        <div>
          {showBack && (
            <Button
              variant="ghost"
              onClick={onBack}
              className="text-muted-foreground hover:text-foreground gap-1.5"
            >
              <ArrowLeft className="h-4 w-4" />
              Back
            </Button>
          )}
        </div>
        <Button
          onClick={onContinue}
          disabled={!canContinue || isLoading}
          className="bg-accent hover:bg-accent/90 text-accent-foreground gap-1.5"
        >
          {isLoading ? "Loading..." : continueLabel}
          {!isLoading && <ArrowRight className="h-4 w-4" />}
        </Button>
      </div>
    </div>
  );
}
