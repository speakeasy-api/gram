import React from "react";
import { StepState } from "./types";

type StepContextApi = {
  step: number;
  state: StepState;
  isCurrentStep: boolean;
  setState: (state: StepState) => void;
};

export const StepContext = React.createContext<StepContextApi>(null!);

export const useStep = () => {
  const ctx = React.useContext(StepContext);

  if (!ctx) {
    throw new Error("useStep must be used within a Step.Provider");
  }

  return ctx;
};
