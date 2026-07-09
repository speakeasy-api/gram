import { useContext } from "react";
import {
  ModalContext,
  type ModalContextType,
} from "@/components/ui/moonshine/context/ModalContext";

export function useModal(): ModalContextType {
  const context = useContext(ModalContext);
  if (context === undefined) {
    throw new Error("useModal must be used within a ModalProvider");
  }
  return context;
}
