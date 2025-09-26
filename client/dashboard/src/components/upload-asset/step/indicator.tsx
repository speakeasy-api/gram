import { cn } from "@/lib/utils";
import { CheckIcon, XIcon } from "lucide-react";
import { memo } from "react";
import { useContext as useStepContext } from "./context";
import { StepState } from "./types";

export default function Indicator() {
  const step = useStepContext();

  return (
    <div
      className={cn(
        "flex items-center justify-center w-7 h-7 rounded-full bg-accent mt-1 [grid-area:indicator]",
        step.isCurrentStep && "bg-primary text-primary-foreground",
        step.state === "completed" &&
          "dark:bg-emerald-900 dark:text-emerald-300 bg-emerald-300 text-emerald-900",
        step.state === "failed" && "bg-destructive text-destructive-foreground",
      )}
    >
      <Content number={step.step} state={step.state} />
    </div>
  );
}

const Content = memo((props: { number: number; state: StepState }) => {
  const { number, state } = props;

  switch (state) {
    case "failed":
      return <XIcon className="size-4" />;
    case "completed":
      return <CheckIcon className="size-4" />;
    case "idle":
      return <span>{number}</span>;
  }
});
