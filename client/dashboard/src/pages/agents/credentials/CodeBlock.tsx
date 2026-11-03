import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { ConfigContext } from "@/components/ui/context/config";
import type { Theme } from "@/components/ui/context/theme";
import { useCallback, useMemo, type CSSProperties, type JSX } from "react";

/**
 * A code block, syntax highlighted and dark whatever the app theme is: these
 * are things to paste into a terminal or a config file, and reading them as
 * code is the point. The design system's snippet follows the app theme, so
 * the theme it reads is overridden here rather than forked.
 */
export function CodeBlock({
  value,
  label,
  language,
  breakAnywhere = false,
}: {
  value: string;
  label: string;
  language: string;
  /**
   * Break mid-token. Wrapping is by whitespace, so a single long value — a
   * key — overflows instead, and takes the copy button off the edge with it.
   */
  breakAnywhere?: boolean;
}): JSX.Element {
  const setTheme = useCallback(() => undefined, []);
  const dark = useMemo(
    () => ({ theme: "dark" as Theme, setTheme }),
    [setTheme],
  );
  return (
    <div className="space-y-2">
      <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
        {label}
      </span>
      {/* Named, because highlighting splits the code into token elements and
          the block is the only thing that still reads as one value.
          The dark tokens are declared on :root.dark, which nothing nested can
          match, so the snippet's surface stays light however its highlighting
          is themed. These are the three it paints with, at their dark values,
          scoped to this block. */}
      <div
        role="group"
        aria-label={label}
        style={
          {
            "--card": "var(--color-neutral-900)",
            "--muted": "var(--color-neutral-900)",
            "--text-body": "var(--color-neutral-200)",
          } as CSSProperties
        }
      >
        <ConfigContext.Provider value={dark}>
          <CodeSnippet
            code={value}
            language={language}
            copyable
            // Every block carries a copy button, so each one says which.
            copyLabel={`Copy ${label}`}
            wordWrap
            snippetClassName={breakAnywhere ? "break-all" : undefined}
          />
        </ConfigContext.Provider>
      </div>
    </div>
  );
}
