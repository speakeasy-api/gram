import { ElementsContextType, ElementsProviderProps } from "@/types";
import { createContext, useContext } from "react";

const ElementsContext = createContext<ElementsContextType>({});

export const ElementsProvider = ({ children }: ElementsProviderProps) => {
  return (
    <ElementsContext.Provider value={{}}>{children}</ElementsContext.Provider>
  );
};

export const useElements = () => {
  const context = useContext(ElementsContext);
  if (!context) {
    throw new Error("useElements must be used within a ElementsProvider");
  }
  return context;
};
