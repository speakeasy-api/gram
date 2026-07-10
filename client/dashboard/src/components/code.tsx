import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/moonshine";
import { getLanguageAccentColor } from "@/components/ui/moonshine/lib/codeUtils";
import { Check, Copy } from "lucide-react";
import React, { useEffect } from "react";
import {
  BuiltinTheme,
  type BundledLanguage,
  codeToHtml,
  codeToTokens,
  type ThemedToken,
} from "shiki";

// Code blocks always render on the ink brand surface (Claude Design), so
// they always highlight with the dark Shiki theme regardless of app theme.
const SHIKI_THEME: BuiltinTheme = "github-dark-default";

/**
 * A slot lets the caller host an interactive React node inline inside the
 * highlighted code, in place of a sentinel string. Keyed by the exact token
 * text shiki produces for the sentinel — for a JSON value that's the quoted
 * form, e.g. `"\"__SLOT_orgToken__\""`. Used opt-in: passing `slots` switches
 * CodeBlock to a token-rendered path (real React nodes) instead of the default
 * dangerouslySetInnerHTML path, so existing usages are unaffected.
 */
export type CodeBlockSlot = {
  /** React node rendered inline where the sentinel token was. */
  node: React.ReactNode;
  /** Text substituted for the sentinel when copying, so the clipboard stays
   *  valid while the slot is unfilled. Defaults to "". */
  copyText?: string;
};

// shiki FontStyle is a bitmask: Italic=1, Bold=2, Underline=4 (None=0/-1).
function fontStyleProps(fs?: number): React.CSSProperties {
  if (!fs || fs < 0) return {};
  return {
    fontStyle: fs & 1 ? "italic" : undefined,
    fontWeight: fs & 2 ? "bold" : undefined,
    textDecoration: fs & 4 ? "underline" : undefined,
  };
}

export function CodeBlock({
  children: code,
  language,
  className,
  innerClassName,
  copyable = true,
  onCopy,
  preClassName,
  slots,
  surface = "dark",
}: {
  children: string;
  language?: string;
  className?: string;
  innerClassName?: string;
  copyable?: boolean;
  onCopy?: () => void;
  preClassName?: string;
  slots?: Record<string, CodeBlockSlot>;
  /**
   * Fixed surface tone, independent of the app theme (matching the Shiki
   * theme, which is always dark — see below). Default `"dark"` is the
   * brandbook ink block. `"light"` is for small inline snippets that need to
   * sit inside a lighter panel (e.g. a single-line URL chip in a card) — it
   * has no syntax highlighting affordance of its own, so only use it when
   * `language` is unset (plain-text fallback rendering).
   */
  surface?: "dark" | "light";
}): React.JSX.Element {
  const hasSlots = !!slots && Object.keys(slots).length > 0;
  const accentColor = getLanguageAccentColor(language);
  const innerStyle: React.CSSProperties = { borderLeftColor: accentColor };
  const [highlightedCode, setHighlightedCode] = React.useState<string | null>(
    null,
  );
  const [tokenResult, setTokenResult] = React.useState<Awaited<
    ReturnType<typeof codeToTokens>
  > | null>(null);
  const [copied, setCopied] = React.useState(false);

  // What the copy button actually writes: with slots, substitute each sentinel
  // with its copyText so an unfilled inline action never leaks the sentinel.
  const copyText = React.useMemo(() => {
    if (!slots) return code;
    let out = code;
    for (const [sentinel, slot] of Object.entries(slots)) {
      out = out.split(sentinel).join(slot.copyText ?? "");
    }
    return out;
  }, [code, slots]);

  const handleCopy = () => {
    void navigator.clipboard.writeText(copyText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
    onCopy?.();
  };

  useEffect(() => {
    if (!language) return;
    let cancelled = false;

    if (hasSlots) {
      // codeToTokens types `lang` stricter than codeToHtml's loose string; the
      // prop is a free-form string, so narrow it the same way callers expect.
      void codeToTokens(code, {
        lang: language as BundledLanguage,
        theme: SHIKI_THEME,
      }).then((res) => {
        if (!cancelled) setTokenResult(res);
      });
    } else {
      void codeToHtml(code, {
        lang: language,
        theme: SHIKI_THEME,
        transformers: [
          {
            pre(node) {
              // the github shiki themes come with a pre-defined background color, we don't want that
              node.properties.class = cn(
                "!bg-transparent",
                preClassName,
                "dark",
              );
            },
          },
        ],
      }).then((html) => {
        if (!cancelled) setHighlightedCode(html);
      });
    }

    return () => {
      cancelled = true;
    };
  }, [code, language, preClassName, hasSlots]);

  // Claude Design brandbook code-block specimen: ink surface, bone text,
  // mono weight 300 at ~15px/1.6, a 4px language-accent rail on the left
  // instead of an all-around border, ~20px/24px padding. `surface="light"`
  // swaps to the paired fixed-light tokens for inline chips on lighter panels.
  const baseClasses = cn(
    "font-mono font-light text-[15px] leading-[1.6] text-wrap overflow-x-auto border-l-4 break-all whitespace-pre-wrap truncate",
    surface === "light"
      ? "bg-surface-secondary-fixed-light text-default-fixed-dark"
      : "bg-surface-secondary-fixed-dark text-default-fixed-light",
  );
  const innerClasses = cn(baseClasses, "py-5 px-6 pr-12", innerClassName);
  // Shown until the async highlight resolves (and as the slot-path placeholder).
  const fallback = (
    <div className={innerClasses} style={innerStyle}>
      {code}
    </div>
  );

  // Token-rendered path: shiki tokens as real React nodes so a slot can host an
  // interactive element inline. Mirrors shiki's <pre><code> structure so the
  // visual matches the default path.
  const renderTokens = (result: Awaited<ReturnType<typeof codeToTokens>>) => {
    const lines = result.tokens as ThemedToken[][];
    return (
      <pre
        className={cn(
          "!bg-transparent break-all whitespace-pre-wrap",
          preClassName,
        )}
        style={{ color: result.fg }}
      >
        <code>
          {lines.map((line, li) => (
            <React.Fragment key={li}>
              {line.map((tok, ti) => {
                // Match by substring rather than exact token text: the caller
                // keys slots by a plain sentinel (e.g. "__SLOT_orgToken__") and
                // shiki may wrap it (quotes, etc.) in the token it emits, so the
                // page doesn't have to know shiki's exact tokenization.
                const slotKey =
                  slots &&
                  Object.keys(slots).find((k) => tok.content.includes(k));
                if (slotKey) {
                  return (
                    <React.Fragment key={ti}>
                      {slots[slotKey]!.node}
                    </React.Fragment>
                  );
                }
                return (
                  <span
                    key={ti}
                    style={{
                      color: tok.color,
                      ...fontStyleProps(tok.fontStyle),
                    }}
                  >
                    {tok.content}
                  </span>
                );
              })}
              {li < lines.length - 1 ? "\n" : null}
            </React.Fragment>
          ))}
        </code>
      </pre>
    );
  };

  return (
    <div className={cn("group relative", className)}>
      {hasSlots ? (
        tokenResult ? (
          <div className={innerClasses} style={innerStyle}>
            {renderTokens(tokenResult)}
          </div>
        ) : (
          fallback
        )
      ) : highlightedCode ? (
        <div
          className={innerClasses}
          style={innerStyle}
          dangerouslySetInnerHTML={{ __html: highlightedCode ?? "" }}
        />
      ) : (
        fallback
      )}
      {copyable && (
        <Button
          variant="tertiary"
          size="sm"
          onClick={handleCopy}
          className="absolute top-1/2 right-2 -translate-y-1/2 p-2"
        >
          <Button.LeftIcon>
            {copied ? (
              <Check className="h-4 w-4" />
            ) : (
              <Copy className="h-4 w-4" />
            )}
          </Button.LeftIcon>
          <Button.Text className="sr-only">
            {copied ? "Copied" : "Copy code"}
          </Button.Text>
        </Button>
      )}
    </div>
  );
}
