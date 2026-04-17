import { cn } from "@/lib/utils";
import React from "react";
import Step from "./index";
import { useStep } from "./use-step";

type FrameProps = {
  children: React.ReactNode;
};

export default function Frame({ children }: FrameProps) {
  const step = useStep();

  const header = React.Children.toArray(children).find(
    (child) => React.isValidElement(child) && child.type === Step.Header,
  );

  const indicator = React.Children.toArray(children).find(
    (child) => React.isValidElement(child) && child.type === Step.Indicator,
  );

  const content = React.Children.toArray(children).find(
    (child) => React.isValidElement(child) && child.type === Step.Content,
  );

  return (
    <div
      className={cn(
        "trans flex w-full flex-row flex-nowrap items-stretch justify-start gap-4 p-0",
        step.isCurrentStep ? "opacity-100" : "opacity-50",
      )}
    >
      {indicator}
      <div className="flex w-full flex-col flex-nowrap items-stretch justify-start gap-4 p-0">
        {header}
        {content}
      </div>
    </div>
  );
}
