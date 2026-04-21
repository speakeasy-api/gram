import React from "react";
import { useStepper as useStepperContext } from "../stepper/use-stepper";
import { StepContext } from "./use-step";
import { StepState } from "./types";

type StepContextProviderProps = {
  step: number;
  children: React.ReactNode;
};

export const StepContextProvider: React.FC<StepContextProviderProps> = (
  props,
) => {
  const stepper = useStepperContext();
  const [isCurrentStep, setIsCurrentStep] = React.useState(false);
  const [state, setState] = React.useState<StepState>("idle");

  React.useEffect(() => {
    const unsubsribe = stepper.subscribe((step) => {
      setIsCurrentStep(step === props.step);
    });

    return unsubsribe;
  }, [stepper, props.step]);

  return (
    <StepContext.Provider
      value={{ step: props.step, state, setState, isCurrentStep }}
    >
      {props.children}
    </StepContext.Provider>
  );
};
