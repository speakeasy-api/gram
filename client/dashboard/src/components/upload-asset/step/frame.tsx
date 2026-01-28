import { cn } from "@/lib/utils";
import React from "react";
import Step from "./index";
import { useStep } from "./context";

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
        "flex flex-row gap-4 p-0 flex-nowrap items-stretch justify-start trans w-full",
        step.isCurrentStep ? "opacity-100" : "opacity-50",
      )}
    >
      {indicator}
      <div className="flex flex-col gap-4 p-0 flex-nowrap items-stretch justify-start w-full">
        {header}
        {content}
      </div>
    </div>
  );
}
