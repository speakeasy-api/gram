import React, { useEffect } from "react";
import { codeToHtml, BuiltinTheme } from "shiki";
import { Check, Copy } from "lucide-react";
import { Button } from "@speakeasy-api/moonshine";
import { cn } from "@/lib/utils";

const DEFAULT_THEME: BuiltinTheme = "github-light-default";

export function CodeBlock({
  children: code,
  language,
  className,
  copyable = true,
  onCopy,
  preClassName,
}: {
  children: string;
  language?: string;
  className?: string;
  copyable?: boolean;
  onCopy?: () => void;
  preClassName?: string;
}) {
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
      theme: DEFAULT_THEME,
      transformers: [
        {
          pre(node) {
            node.properties.class = preClassName;
          },
        },
      ],
    }).then(setHighlightedCode);
  }, [code, language, preClassName]);

  return (
    <div className="relative group">
      {highlightedCode ? (
        <div
          className={cn(
            "p-4 rounded-md font-mono text-sm text-wrap overflow-x-auto border whitespace-pre-wrap break-all pr-12",
            className,
          )}
          dangerouslySetInnerHTML={{ __html: highlightedCode ?? "" }}
        />
      ) : (
        <div className="p-4 rounded-md font-mono text-sm text-wrap overflow-x-auto border break-all whitespace-pre-wrap truncate pr-12">
          {code}
        </div>
      )}
      {copyable && (
        <Button
          variant="tertiary"
          size="sm"
          onClick={handleCopy}
          className="absolute top-1/2 -translate-y-1/2 right-2 p-2"
        >
          <Button.LeftIcon>
            {copied ? (
              <Check className="w-4 h-4" />
            ) : (
              <Copy className="w-4 h-4" />
            )}
          </Button.LeftIcon>
        </Button>
      )}
    </div>
  );
}
