import React from "react";
import { useContext as useStepperContext } from "../stepper/context";
import { StepState } from "./types";

type ContextApi = {
  step: number;
  state: StepState;
  isCurrentStep: boolean;
  setState: (state: StepState) => void;
};

const Context = React.createContext<ContextApi>(null!);

type ProviderProps = {
  step: number;
  children: React.ReactNode;
};

export const Provider: React.FC<ProviderProps> = (props) => {
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
    <Context.Provider
      value={{ step: props.step, state, setState, isCurrentStep }}
    >
      {props.children}
    </Context.Provider>
  );
};

export const useContext = () => {
  return React.useContext(Context);
};
