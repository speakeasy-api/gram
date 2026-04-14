import { useEffect, useRef, useState } from "react";
import mermaid from "mermaid";

mermaid.initialize({
  startOnLoad: false,
  theme: "default",
  securityLevel: "loose",
  fontFamily: "system-ui, sans-serif",
});

let counter = 0;

export function Mermaid({ source }: { source: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const id = `mmd-${++counter}`;

    mermaid
      .render(id, source)
      .then(({ svg }) => {
        if (cancelled) return;
        setError(null);
        if (ref.current) ref.current.innerHTML = svg;
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      });

    return () => {
      cancelled = true;
    };
  }, [source]);

  if (error) {
    return <pre className="error">{error}</pre>;
  }

  return <div ref={ref} className="mermaid-container" aria-label="diagram" />;
}
