import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import type { ReactNode } from "react";

export function RequirePluginWrite({ children }: { children: ReactNode }) {
  return usePluginWriteAccess() ? <>{children}</> : null;
}
