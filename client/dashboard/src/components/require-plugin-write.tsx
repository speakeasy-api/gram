import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import type { JSX, ReactNode } from "react";

export function RequirePluginWrite({
  children,
}: {
  children: ReactNode;
}): JSX.Element | null {
  return usePluginWriteAccess() ? <>{children}</> : null;
}
