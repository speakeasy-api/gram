import { useState } from "react";

import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import type { Theme } from "@/components/ui/context/theme";
import { TooltipProvider } from "@/components/ui/Tooltip";

/**
 * Mirrors the providers \`App.tsx\` wraps the dashboard in, so components behave
 * in the gallery the way they do in the product.
 */
export function DesignSystemProviders({
  initialTheme,
  children,
}: {
  initialTheme: Theme;
  children: React.ReactNode;
}): React.JSX.Element {
  const [theme, setTheme] = useState<Theme>(initialTheme);

  return (
    <ConfigProvider theme={theme} setTheme={setTheme}>
      <TooltipProvider>{children}</TooltipProvider>
    </ConfigProvider>
  );
}
