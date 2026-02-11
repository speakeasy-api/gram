import { cn } from "@/lib/utils";
import { CheckIcon, CopyIcon, ChevronDownIcon } from "lucide-react";
import { useState, useMemo } from "react";

/** Max lines shown in the collapsed preview */
const PREVIEW_LINE_LIMIT = 50;

function truncateToLines(text: string, maxLines: number) {
  let pos = 0;
  for (let i = 0; i < maxLines; i++) {
    const next = text.indexOf("\n", pos);
    if (next === -1) return { text, truncated: false, totalLines: i + 1 };
    pos = next + 1;
  }
  const totalLines = text.split("\n").length;
  return { text: text.slice(0, pos), truncated: true, totalLines };
}

function CopyButton({ content }: { content: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    await navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      onClick={handleCopy}
      className="text-slate-400 hover:text-slate-200 rounded p-1 transition-colors"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <CheckIcon className="size-4" />
      ) : (
        <CopyIcon className="size-4" />
      )}
    </button>
  );
}

interface CodeBlockProps {
  /** The content to display - can be string or object (will be JSON stringified) */
  content: string | Record<string, unknown> | unknown;
  /** Optional title/label for the code block */
  title?: string;
  /** Whether to start expanded (default: false) */
  defaultExpanded?: boolean;
  /** Additional class names */
  className?: string;
  /** Max height in pixels (default: 300) */
  maxHeight?: number;
}

export function CodeBlock({
  content,
  title,
  defaultExpanded = false,
  className,
  maxHeight = 300,
}: CodeBlockProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  // Format the content as a string
  const formattedContent = useMemo(() => {
    if (typeof content === "string") {
      // Try to parse and re-format if it's JSON
      try {
        return JSON.stringify(JSON.parse(content), null, 2);
      } catch {
        return content;
      }
    }
    return JSON.stringify(content, null, 2);
  }, [content]);

  const preview = useMemo(
    () => truncateToLines(formattedContent, PREVIEW_LINE_LIMIT),
    [formattedContent],
  );

  const displayText = expanded ? formattedContent : preview.text;
  const canExpand = preview.truncated && !expanded;

  return (
    <div className={cn("w-full rounded-lg overflow-hidden", className)}>
      {title && (
        <div className="flex items-center justify-between bg-slate-900 px-4 py-2 border-b border-slate-700">
          <span className="text-xs font-medium text-slate-400">{title}</span>
          <CopyButton content={formattedContent} />
        </div>
      )}
      <div className="relative">
        <pre
          className={cn(
            "bg-slate-800/90 px-4 py-3 text-sm text-slate-100 font-mono whitespace-pre-wrap break-all overflow-y-auto",
            !title && "rounded-t-lg",
          )}
          style={{ maxHeight: `${maxHeight}px` }}
        >
          {displayText}
        </pre>
        {!title && (
          <div className="absolute top-2 right-2">
            <CopyButton content={formattedContent} />
          </div>
        )}
      </div>
      {canExpand && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="w-full bg-slate-900 px-4 py-2 text-left text-xs text-slate-400 transition-colors hover:text-slate-200 hover:bg-slate-800 flex items-center gap-1"
        >
          <ChevronDownIcon className="size-3" />
          Show all {preview.totalLines} lines…
        </button>
      )}
    </div>
  );
}

/** Collapsible section with code block - similar to Elements ToolUISection */
interface CollapsibleCodeSectionProps {
  title: string;
  content: string | Record<string, unknown> | unknown;
  defaultExpanded?: boolean;
}

export function CollapsibleCodeSection({
  title,
  content,
  defaultExpanded = false,
}: CollapsibleCodeSectionProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const formattedContent = useMemo(() => {
    if (typeof content === "string") {
      try {
        return JSON.stringify(JSON.parse(content), null, 2);
      } catch {
        return content;
      }
    }
    return JSON.stringify(content, null, 2);
  }, [content]);

  return (
    <div className="border-t border-border">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="hover:bg-muted/50 flex w-full cursor-pointer items-center justify-between px-4 py-2.5 text-left transition-colors"
      >
        <span className="text-muted-foreground text-sm">{title}</span>
        <div className="flex items-center gap-1">
          <CopyButton content={formattedContent} />
          <ChevronDownIcon
            className={cn(
              "text-muted-foreground size-4 transition-transform duration-200",
              isExpanded && "rotate-180",
            )}
          />
        </div>
      </button>
      {isExpanded && (
        <div className="border-t border-border">
          <CodeBlock content={content} />
        </div>
      )}
    </div>
  );
}
