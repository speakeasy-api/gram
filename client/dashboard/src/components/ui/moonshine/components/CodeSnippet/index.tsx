import { useCallback, useState, useEffect, useRef } from "react";
import { cn } from "@/components/ui/moonshine/lib/utils";
import { ProgrammingLanguage, Size } from "@/components/ui/moonshine/types";
import { AnimatePresence, motion } from "motion/react";
import "@/components/ui/moonshine/styles/codeSyntax.css";
import "./codeSnippet.css";
import { Icon } from "../Icon";
import {
  highlightCode,
  HighlightedCode,
  LIGHT_THEME,
  DARK_THEME,
} from "@/components/ui/moonshine/lib/codeUtils";
import { useConfig } from "@/components/ui/moonshine/hooks/useConfig";
import { Pre } from "../CodeHighlight/Pre";
import { preventDefault } from "@/components/ui/moonshine/lib/events";

export interface CodeSnippetProps {
  /**
   * The code to display.
   */
  code: string;
  /**
   * Whether to show a copy button.
   */
  copyable?: boolean;
  /**
   * One of the known Speakeasy target languages, or a language that Shiki supports.
   * The full list of supported languages is available at https://shiki.style/languages
   */
  language: ProgrammingLanguage | (string & {});
  /**
   * The symbol to display before the code.
   */
  promptSymbol?: React.ReactNode;
  /**
   * Whether to display the code snippet inline.
   */
  inline?: boolean;
  /**
   * The font size of the code snippet.
   */
  fontSize?: Size;
  /**
   * Whether to show line numbers.
   */
  showLineNumbers?: boolean;
  /**
   * The callback to call when the code is selected or copied.
   */
  onSelectOrCopy?: () => void;
  /**
   * Whether to shimmer the code snippet.
   */
  shimmer?: boolean;
  /**
   * Additional CSS classes to apply to the code snippet container
   */
  className?: string;

  /**
   * Additional CSS classes to apply to the code snippet inner container (e.g the Pre component).
   */
  snippetClassName?: string;
}

const fontSizeMap: Record<Size, string> = {
  small: "text-sm",
  medium: "text-sm",
  large: "text-base",
  xl: "text-lg",
  "2xl": "text-xl",
};

const copyIconVariants = {
  hidden: { opacity: 0, scale: 0.5 },
  visible: { opacity: 1, scale: 1 },
};

export function CodeSnippet({
  code,
  copyable = false,
  language,
  promptSymbol,
  inline = false,
  fontSize = "medium",
  onSelectOrCopy,
  shimmer = false,
  className,
  snippetClassName,
  showLineNumbers = false,
}: CodeSnippetProps): React.JSX.Element {
  const [copying, setCopying] = useState(false);
  const [containerWidth, setContainerWidth] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const updateWidth = () => {
      if (containerRef.current) {
        const width = containerRef.current.getBoundingClientRect().width;
        // Only update if we have a non-zero width
        if (width > 0) {
          setContainerWidth(width);
        }
      }
    };

    // Initial measurement
    updateWidth();

    // Create ResizeObserver for more reliable width tracking
    const resizeObserver = new ResizeObserver(updateWidth);
    if (containerRef.current) {
      resizeObserver.observe(containerRef.current);
    }

    return () => resizeObserver.disconnect();
  }, []);

  const [highlightedCodeState, setHighlightedCodeState] = useState<
    HighlightedCode | undefined
  >(undefined);
  const isMultiline = code.split("\n").length > 1;
  const { theme } = useConfig();

  // Directly highlight the code when code or language changes
  useEffect(() => {
    if (!language) return;

    const shikiTheme = theme === "dark" ? DARK_THEME : LIGHT_THEME;
    // Use the highlightCode utility directly
    void highlightCode(code, language, shikiTheme).then((highlighted) => {
      setHighlightedCodeState(highlighted);
    });
  }, [code, language, theme]);

  const handleCopy = useCallback(() => {
    setCopying(true);
    void navigator.clipboard.writeText(highlightedCodeState?.code ?? code);
    setTimeout(() => {
      setCopying(false);
      onSelectOrCopy?.();
    }, 1000);
  }, [highlightedCodeState?.code, code, onSelectOrCopy]);

  return (
    <div
      data-theme={theme}
      className={cn(
        "snippet relative box-border flex w-full overflow-hidden rounded-lg border border-muted bg-card",
        inline && "inline-flex",
        shimmer && "shimmer",
        className,
      )}
      style={{ "--width": `${containerWidth}px` } as React.CSSProperties}
      ref={containerRef}
    >
      <div className="snippet-inner flex w-full flex-row gap-2 rounded-lg bg-card p-4">
        {language === "bash" && (
          <div className="self-center font-mono font-light text-body select-none">
            {promptSymbol ?? "$"}
          </div>
        )}
        {highlightedCodeState && (
          <Pre
            code={highlightedCodeState}
            onClick={onSelectOrCopy}
            className={cn(
              "highlighted-code inline-flex w-fit self-center font-mono outline-none",
              fontSizeMap[fontSize],
              isMultiline && "min-w-32",
              snippetClassName,
            )}
            onBeforeInput={preventDefault}
            showLineNumbers={showLineNumbers}
          />
        )}

        {copyable && (
          <div
            className={cn(
              "mr-1 ml-auto flex self-center text-white",
              isMultiline && "mt-1 h-4 w-6 self-start",
            )}
          >
            <button
              role="button"
              aria-label="copy"
              className="relative ml-2 border-none bg-transparent outline-none"
              onClick={handleCopy}
            >
              <AnimatePresence mode="wait" initial={false}>
                {copying ? (
                  <motion.span
                    key="checkmark"
                    variants={copyIconVariants}
                    initial="hidden"
                    animate="visible"
                    className="text-green-500"
                  >
                    <Icon name="check" stroke="currentColor" />
                  </motion.span>
                ) : (
                  <motion.span
                    key="copy"
                    variants={copyIconVariants}
                    initial="hidden"
                    animate="visible"
                    className="text-body-muted hover:text-body"
                    exit="hidden"
                  >
                    <Icon name="copy" stroke="currentColor" />
                  </motion.span>
                )}
              </AnimatePresence>
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
