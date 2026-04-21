import type {
  Deployment,
  Toolset,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import React from "react";

type StepperSubscriber = (cb: (step: number) => void) => () => void;

type StepperContextApiMeta = {
  file: File | null;
  uploadResult: UploadOpenAPIv3Result | null;
  assetName: string | null;
  deployment: Deployment | null;
  toolset: Toolset | null;
};

type StepperContextApi = {
  /* Initial step number. */
  step: number;
  /* Subscriber for step changes */
  subscribe: StepperSubscriber;
  /* Function to register a step */
  registerStep: (step: number) => void;
  /* Current state of the stepper */
  state: "idle" | "completed" | "error";
  /* Function to set the state of the stepper */
  setState: React.Dispatch<
    React.SetStateAction<"idle" | "completed" | "error">
  >;
  /* Go to next step */
  next: () => void;
  /* Reset to initial state */
  reset: () => void;
  /* Meta information shared between steps */
  meta: React.RefObject<StepperContextApiMeta>;
};

export const StepperContext = React.createContext<StepperContextApi>(null!);

export const useStepper = () => {
  const ctx = React.useContext(StepperContext);
  if (!ctx) throw new Error("useStep must be used within a Stepper.Provider");
  return ctx;
};
