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

const StepperContext = React.createContext<StepperContextApi>(null!);

type StepperContextProviderProps = {
  children: React.ReactNode;
  step: number;
};

export const StepperContextProvider: React.FC<StepperContextProviderProps> = ({
  step: initialStep,
  children,
}) => {
  const [state, setState] = React.useState<"idle" | "completed" | "error">(
    "idle",
  );

  const meta = React.useRef<StepperContextApiMeta>({
    file: null,
    uploadResult: null,
    assetName: null,
    deployment: null,
    toolset: null,
  });

  const steps = React.useRef<Set<number>>(new Set());
  const step = React.useRef(initialStep);
  const subscribers = React.useRef<Set<(step: number) => void>>(new Set());
  const isMounted = React.useRef(true);

  const registerStep = React.useCallback((step: number) => {
    steps.current.add(step);
  }, []);

  React.useEffect(() => {
    isMounted.current = true;
    return () => {
      isMounted.current = false;
      subscribers.current.clear();
    };
  }, []);

  /**
   * Subscribe to step changes. The callback is called immediately with the
   * current step.
   */
  const subscribe = React.useCallback<StepperSubscriber>((cb) => {
    subscribers.current.add(cb);
    cb(step.current);
    return () => {
      subscribers.current.delete(cb);
    };
  }, []);

  const next = React.useCallback(() => {
    // Prevent state updates if called after unmount (eg: from a lingering async operation)
    if (!isMounted.current) return;

    const nextStep = step.current + 1;
    if (nextStep > Math.max(...Array.from(steps.current))) return;
    step.current = nextStep;
    subscribers.current.forEach((cb) => cb(step.current));
  }, []);

  const reset = React.useCallback(() => {
    step.current = initialStep;
    setState("idle");
    meta.current = {
      file: null,
      uploadResult: null,
      assetName: null,
      deployment: null,
      toolset: null,
    };
    subscribers.current.forEach((cb) => cb(step.current));
  }, [initialStep]);

  return (
    <StepperContext.Provider
      value={{
        get step() {
          return step.current;
        },
        state,
        setState,
        subscribe,
        registerStep,
        next,
        reset,
        meta,
      }}
    >
      {children}
    </StepperContext.Provider>
  );
};

export const useStepper = () => {
  const ctx = React.useContext(StepperContext);
  if (!ctx) throw new Error("useStep must be used within a Stepper.Provider");
  return ctx;
};
