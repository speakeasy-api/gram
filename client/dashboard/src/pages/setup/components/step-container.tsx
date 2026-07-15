import type { ReactNode } from "react";
import { ArrowRight, ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";

interface StepContainerProps {
  icon: ReactNode;
  title: string;
  description: string;
  children: ReactNode;
  onContinue?: () => void;
  onBack?: () => void;
  onSkip?: () => void;
  continueLabel?: string;
  skipLabel?: string;
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
  onSkip,
  continueLabel = "Continue",
  skipLabel = "Skip",
  showBack = false,
  isLoading = false,
  canContinue = true,
}: StepContainerProps): JSX.Element {
  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center gap-0">
        <div className="flex-shrink-0">{icon}</div>
        <Heading variant="h1" className="normal-case">
          {title}
        </Heading>
      </div>
      <Type muted small className="mt-2">
        {description}
      </Type>

      {/* Content */}
      <div className="mt-8 flex-1">{children}</div>

      {/* Divider */}
      <div className="bg-border mt-8 h-px" />

      {/* Actions */}
      <div className="mt-6 flex items-center justify-between">
        <div>
          {showBack && (
            <Button
              variant="tertiary"
              onClick={onBack}
              className="text-muted-foreground hover:text-foreground"
            >
              <Button.LeftIcon>
                <ArrowLeft className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Back</Button.Text>
            </Button>
          )}
        </div>
        <div className="flex items-center gap-3">
          {onSkip && (
            <Button
              variant="tertiary"
              onClick={onSkip}
              className="text-muted-foreground hover:text-foreground"
            >
              {skipLabel}
            </Button>
          )}
          <Button onClick={onContinue} disabled={!canContinue || isLoading}>
            <Button.Text>
              {isLoading ? "Loading..." : continueLabel}
            </Button.Text>
            {!isLoading && (
              <Button.RightIcon>
                <ArrowRight className="h-4 w-4" />
              </Button.RightIcon>
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}
