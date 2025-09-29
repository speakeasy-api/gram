import type {
  Deployment,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import React from "react";

type Subscriber = (cb: (step: number) => void) => () => void;

type ContextApiMeta = {
  file: File | null;
  uploadResult: UploadOpenAPIv3Result | null;
  assetName: string | null;
  deployment: Deployment | null;
};

type ContextApi = {
  /* Initial step number. */
  step: number;
  /* Subscriber for step changes */
  subscribe: Subscriber;
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
  meta: React.RefObject<ContextApiMeta>;
};

const Context = React.createContext<ContextApi>(null!);

type ProviderProps = {
  children: React.ReactNode;
  step: number;
};

export const Provider: React.FC<ProviderProps> = ({
  step: initialStep,
  children,
}) => {
  const [state, setState] = React.useState<"idle" | "completed" | "error">(
    "idle",
  );

  const meta = React.useRef<ContextApiMeta>({
    file: null,
    uploadResult: null,
    assetName: null,
    deployment: null,
  });

  const steps = React.useRef<Set<number>>(new Set());
  const step = React.useRef(initialStep);
  const subscribers = React.useRef<Set<(step: number) => void>>(new Set());

  const registerStep = React.useCallback((step: number) => {
    steps.current.add(step);
  }, []);

  /**
   * Subscribe to step changes. The callback is called immediately with the
   * current step.
   */
  const subscribe = React.useCallback<Subscriber>((cb) => {
    subscribers.current.add(cb);
    cb(step.current);
    return () => {
      subscribers.current.delete(cb);
    };
  }, []);

  const next = React.useCallback(() => {
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
    };
    subscribers.current.forEach((cb) => cb(step.current));
  }, []);

  return (
    <Context.Provider
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
    </Context.Provider>
  );
};

export const useContext = () => {
  return React.useContext(Context);
};
