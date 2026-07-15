import { forwardRef, HTMLAttributes } from "react";
import { HighlightedCode } from "@/components/ui/lib/codeUtils";
import { cn } from "@/lib/utils";

export interface PreProps extends Omit<
  HTMLAttributes<HTMLPreElement>,
  "children"
> {
  code: HighlightedCode;
  showLineNumbers?: boolean;
  wordWrap?: boolean;
}

export const Pre = forwardRef<HTMLPreElement, PreProps>(
  (
    { code, showLineNumbers = false, wordWrap = false, className, ...props },
    ref,
  ) => {
    return (
      <pre
        ref={ref}
        className={cn(
          "inline-block font-mono outline-none",
          wordWrap && "whitespace-pre-wrap",
          className,
        )}
        {...props}
      >
        <code>
          {code.lines.map((line, lineIndex) => (
            <div
              key={lineIndex}
              className={cn("flex flex-row", wordWrap && "flex-wrap")}
            >
              {showLineNumbers && (
                <span className="pr-3 text-muted-fixed-light select-none">
                  {lineIndex + 1}
                </span>
              )}
              <span className={cn("inline-block", wordWrap && "flex-1")}>
                {line.tokens.map((token, tokenIndex) => (
                  <span
                    key={tokenIndex}
                    style={{
                      color: token.color,
                      fontStyle: token.fontStyle ? "italic" : undefined,
                      fontWeight:
                        token.fontStyle && token.fontStyle & 1
                          ? "bold"
                          : undefined,
                    }}
                  >
                    {token.content}
                  </span>
                ))}
              </span>
            </div>
          ))}
        </code>
      </pre>
    );
  },
);

Pre.displayName = "Pre";
