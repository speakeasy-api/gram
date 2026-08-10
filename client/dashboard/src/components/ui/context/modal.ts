import { createContext, type ReactNode } from "react";

export type Screen = {
  id: string;
  title: string;
  component: ReactNode;
};

export type ModalContextType = {
  screens: Screen[];
  currentIndex: number;
  isOpen: boolean;
  openScreen: (screen: Screen) => void;
  close: () => void;
  navigateTo: (index: number) => void;
  pushScreen: (screen: Screen) => void;
  popScreen: () => void;
  navigationDirection: "forward" | "backward";
};

export const ModalContext = createContext<ModalContextType | undefined>(
  undefined,
);
