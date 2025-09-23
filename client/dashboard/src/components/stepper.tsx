import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { CheckIcon, XIcon } from "lucide-react";
import { Button } from "./ui/button";
import { Heading } from "./ui/heading";
import { Type } from "./ui/type";

// Step number component for the left side
function StepNumber({
  number,
  completed,
  active,
  failed,
}: {
  number: number;
  completed: boolean;
  active?: boolean;
  failed?: boolean;
}) {
  const Content = () => {
    if (failed) return <XIcon className="size-4" />;
    if (completed) return <CheckIcon className="size-4" />;
    return <span>{number}</span>;
  };

  return (
    <div
      className={cn(
        "flex items-center justify-center w-7 h-7 rounded-full bg-accent mt-1",
        active && "bg-primary text-primary-foreground",
        completed &&
          "dark:bg-emerald-900 dark:text-emerald-300 bg-emerald-300 text-emerald-900",
        failed && "bg-destructive text-destructive-foreground",
      )}
    >
      <Content />
    </div>
  );
}

export type StepProps = {
  heading: string;
  description: string;
  display: React.ReactNode;
  displayComplete: React.ReactNode;
  isComplete: boolean;
  failed?: boolean;
};

export function Stepper({
  steps,
  onComplete,
}: {
  steps: StepProps[];
  onComplete?: () => void;
}) {
  const activeStep =
    steps.findIndex((step) => !step.isComplete) + 1 || steps.length;
  const allCompleted = steps.every((step) => step.isComplete);

  return (
    <div className="w-full">
      {steps.map((step, index) => (
        <Step
          key={index}
          stepNumber={index + 1}
          activeStep={activeStep}
          heading={step.heading}
          description={step.description}
          display={step.display}
          displayComplete={step.displayComplete}
          isComplete={step.isComplete}
          failed={step.failed}
        />
      ))}
      {onComplete && allCompleted && (
        <Button
          onClick={onComplete}
          className="ml-12"
          size={"lg"}
          iconAfter
          icon={"arrow-right"}
        >
          Continue
        </Button>
      )}
    </div>
  );
}

export function Step({
  stepNumber,
  heading,
  description,
  display,
  displayComplete,
  isComplete,
  failed,
  activeStep,
}: StepProps & {
  stepNumber: number;
  activeStep: number;
}) {
  const isActive = stepNumber === activeStep;

  return (
    <Stack
      gap={4}
      direction={"horizontal"}
      className={cn(
        "trans opacity-50 mb-4 w-full",
        isActive && "opacity-100 mb-8",
      )}
    >
      <StepNumber
        number={stepNumber}
        completed={isComplete}
        active={stepNumber === activeStep}
        failed={failed}
      />
      <Stack gap={2} className="mb-4 w-full">
        <Heading variant="h2">{heading}</Heading>
        <Type variant="subheading" muted className="mb-4">
          {description}
        </Type>
        {isComplete ? displayComplete : isActive ? display : null}
      </Stack>
    </Stack>
  );
}
