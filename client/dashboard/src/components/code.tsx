import { cn } from "@/lib/utils";
import { Button, Theme, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Check, Copy } from "lucide-react";
import React, { useEffect } from "react";
import { BuiltinTheme, codeToHtml } from "shiki";

const DEFAULT_THEME_PER_MODE: Record<Theme, BuiltinTheme> = {
  light: "github-light-default",
  dark: "github-dark-default",
};

export function CodeBlock({
  children: code,
  language,
  className,
  innerClassName,
  copyable = true,
  onCopy,
  preClassName,
}: {
  children: string;
  language?: string;
  className?: string;
  innerClassName?: string;
  copyable?: boolean;
  onCopy?: () => void;
  preClassName?: string;
}) {
  const { theme } = useMoonshineConfig();
  const [highlightedCode, setHighlightedCode] = React.useState<string | null>(
    null,
  );
  const [copied, setCopied] = React.useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
    onCopy?.();
  };

  useEffect(() => {
    if (!language) return;

    codeToHtml(code, {
      lang: language,
      theme: DEFAULT_THEME_PER_MODE[theme],
      transformers: [
        {
          pre(node) {
            // the github shiki themes come with a pre-defined background color, we don't want that
            node.properties.class = cn(
              "!bg-transparent",
              preClassName,
              theme === "dark" ? "dark" : "light",
            );
          },
        },
      ],
    }).then(setHighlightedCode);
  }, [code, language, preClassName]);

  const baseClasses =
    "rounded-md font-mono text-sm text-wrap overflow-x-auto border break-all whitespace-pre-wrap truncate";

  return (
    <div className={cn("group relative", className)}>
      {highlightedCode ? (
        <div
          className={cn(baseClasses, "p-4 pr-12", innerClassName)}
          dangerouslySetInnerHTML={{ __html: highlightedCode ?? "" }}
        />
      ) : (
        <div className={cn(baseClasses, "p-4 pr-12", innerClassName)}>
          {code}
        </div>
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
