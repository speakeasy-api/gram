import { createContext, useState, type ReactNode } from "react";

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

// eslint-disable-next-line react-refresh/only-export-components
export const ModalContext = createContext<ModalContextType | undefined>(
  undefined,
);

export function ModalProvider({
  children,
}: {
  children: ReactNode | ((props: ModalContextType) => ReactNode);
}): React.JSX.Element {
  const [screens, setScreens] = useState<Screen[]>([]);
  const [currentIndex, setCurrentIndex] = useState(0);
  const [isOpen, setIsOpen] = useState(false);
  const [navigationDirection, setNavigationDirection] = useState<
    "forward" | "backward"
  >("forward");

  const openScreen = (screen: Screen) => {
    // Don't reopen if already open to prevent infinite loops
    if (!isOpen) {
      setScreens([screen]);
      setCurrentIndex(0);
      setIsOpen(true);
      setNavigationDirection("forward");
    }
  };

  const close = () => {
    setIsOpen(false);
    // Reset after animation completes
    setTimeout(() => {
      setScreens([]);
      setCurrentIndex(0);
    }, 200);
  };

  const navigateTo = (index: number) => {
    setScreens((prevScreens) => {
      if (index >= 0 && index < prevScreens.length) {
        setNavigationDirection(index > currentIndex ? "forward" : "backward");
        setCurrentIndex(index);
      }
      return prevScreens;
    });
  };

  const pushScreen = (screen: Screen) => {
    setScreens((prev) => {
      const newScreens = [...prev, screen];
      setCurrentIndex(newScreens.length - 1);
      return newScreens;
    });
    setNavigationDirection("forward");
  };

  const popScreen = () => {
    setScreens((prev) => {
      if (prev.length > 1) {
        setNavigationDirection("backward");
        setCurrentIndex((idx) => idx - 1);
        return prev.slice(0, prev.length - 1);
      } else {
        close();
        return prev;
      }
    });
  };

  return (
    <ModalContext.Provider
      value={{
        screens,
        currentIndex,
        isOpen,
        openScreen,
        close,
        navigateTo,
        pushScreen,
        popScreen,
        navigationDirection,
      }}
    >
      {typeof children === "function"
        ? children({
            screens,
            currentIndex,
            isOpen,
            openScreen,
            close,
            navigateTo,
            pushScreen,
            popScreen,
            navigationDirection,
          })
        : children}
    </ModalContext.Provider>
  );
}
