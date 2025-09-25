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
  const [state, setState] = React.useState<StepState>("idle");

  const isCurrentStep = React.useMemo(() => {
    return stepper.step === props.step;
  }, [stepper.step, props.step]);

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
