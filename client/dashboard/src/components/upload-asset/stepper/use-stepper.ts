import type { Deployment } from "@gram/client/models/components/deployment.js";
import type { Toolset } from "@gram/client/models/components/toolset.js";
import type { UploadOpenAPIv3Result } from "@gram/client/models/components/uploadopenapiv3result.js";
import React from "react";

type StepperSubscriber = (cb: (step: number) => void) => () => void;

/**
 * A document already in the project that the upload replaces. Set from the
 * start, before any step runs, when the flow was opened to upload a new
 * version rather than to add a source.
 */
export type ExistingDocument = {
  name: string;
  slug: string;
};

export type StepperContextApiMeta = {
  file: File | null;
  uploadResult: UploadOpenAPIv3Result | null;
  assetName: string | null;
  deployment: Deployment | null;
  toolset: Toolset | null;
  existingDocument: ExistingDocument | null;
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

export const useStepper = (): StepperContextApi => {
  const ctx = React.useContext(StepperContext);
  if (!ctx) throw new Error("useStep must be used within a Stepper.Provider");
  return ctx;
};
