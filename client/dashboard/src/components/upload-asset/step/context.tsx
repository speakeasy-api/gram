import React from "react";
import { useStepper as useStepperContext } from "../stepper/context";
import { StepState } from "./types";

type StepContextApi = {
  step: number;
  state: StepState;
  isCurrentStep: boolean;
  setState: (state: StepState) => void;
};

const StepContext = React.createContext<StepContextApi>(null!);

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
  }, []);

  return (
    <StepContext.Provider
      value={{ step: props.step, state, setState, isCurrentStep }}
    >
      {props.children}
    </StepContext.Provider>
  );
};

export const useStep = () => {
  const ctx = React.useContext(StepContext);

  if (!ctx) {
    throw new Error("useStep must be used within a Step.Provider");
  }

  return ctx;
};
