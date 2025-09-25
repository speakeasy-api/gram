import type {
  Deployment,
  UploadOpenAPIv3Result,
} from "@gram/client/models/components";
import React from "react";

type ContextApiMeta = {
  file: File | null;
  uploadResult: UploadOpenAPIv3Result | null;
  deployment: Deployment | null;
};

type ContextApi = {
  step: number;
  next: () => void;
  meta: React.RefObject<ContextApiMeta>;
};

const Context = React.createContext<ContextApi>(null!);

type ProviderProps = {
  children: React.ReactNode;
  step?: number;
};

export const Provider: React.FC<ProviderProps> = (props) => {
  const [step, setStep] = React.useState<number>(props.step ?? 0);
  const meta = React.useRef<ContextApiMeta>({
    file: null,
    uploadResult: null,
    deployment: null,
  });

  const next = React.useCallback(() => {
    setStep((s) => s + 1);
  }, []);

  return (
    <Context.Provider value={{ step, next, meta }}>
      {props.children}
    </Context.Provider>
  );
};

export const useContext = () => {
  return React.useContext(Context);
};
