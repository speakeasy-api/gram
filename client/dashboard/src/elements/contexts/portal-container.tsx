"use client";

import { type RefObject } from "react";
import { PortalContainerContext } from "./portal-container-context";

export { PortalContainerContext };

export function PortalContainerProvider({
  containerRef,
  children,
}: {
  containerRef: RefObject<HTMLElement | null>;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <PortalContainerContext.Provider value={containerRef}>
      {children}
    </PortalContainerContext.Provider>
  );
}
