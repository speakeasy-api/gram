import { cn } from "@/lib/utils";
import { CheckIcon, XIcon } from "lucide-react";
import React from "react";
import { useStepper } from "../stepper/context";
import { StepContextProvider, useStep } from "./context";
import Frame from "./frame";
import { StepState } from "./types";

export { useStep } from "./context";

type StepProps = {
  children: React.ReactNode;
  step: number;
};

export default function Step({ children, step }: StepProps) {
  const stepper = useStepper();

  stepper.registerStep(step);

  return (
    <StepContextProvider step={step}>
      <Frame>{children}</Frame>
    </StepContextProvider>
  );
}

function Indicator() {
  const step = useStep();

  return (
    <div
      className={cn(
        "bg-accent mt-1 flex h-7 w-7 items-center justify-center rounded-full [grid-area:indicator]",
        step.isCurrentStep && "bg-primary text-primary-foreground",
        step.state === "completed" &&
          "bg-emerald-300 text-emerald-900 dark:bg-emerald-900 dark:text-emerald-300",
        step.state === "failed" && "bg-destructive text-destructive-foreground",
      )}
    >
      <IndicatorContent number={step.step} state={step.state} />
    </div>
  );
}

const IndicatorContent = React.memo(
  (props: { number: number; state: StepState }) => {
    const { number, state } = props;

    switch (state) {
      case "failed":
        return <XIcon className="size-4" />;
      case "completed":
        return <CheckIcon className="size-4" />;
      case "idle":
        return <span>{number}</span>;
    }
  },
);

function Header({
  title,
  description,
}: {
  title: string;
  description?: string;
}) {
  return (
    <div className="[grid-area=header]">
      <h2 className="text-2xl font-normal capitalize">{title}</h2>
      {description && (
        <p className="text-muted-foreground mt-1 text-sm">{description}</p>
      )}
    </div>
  );
}

function Content({ children }: React.PropsWithChildren) {
  return <div>{children}</div>;
}

Step.Indicator = Indicator;
Step.Header = Header;
Step.Content = Content;
