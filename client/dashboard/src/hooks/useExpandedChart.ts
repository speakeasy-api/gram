import { useEffect, useState } from "react";

export function useExpandedChart() {
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  useEffect(() => {
    if (!expandedChart) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) setExpandedChart(null);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [expandedChart]);

  return { expandedChart, setExpandedChart };
}
