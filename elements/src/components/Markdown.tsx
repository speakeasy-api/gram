import { type FC, memo, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";

export interface MarkdownProps {
  /** Raw markdown text. */
  children: string;
  /** Optional className applied to the `.aui-md` root wrapper. */
  className?: string;
}

/**
 * Standalone markdown renderer that mirrors the look of the live
 * `<MarkdownText />` (the same `aui-md-*` typography) but renders a plain
 * string with `react-markdown` instead of the assistant-ui streaming runtime.
 *
 * Use in static viewers (the dashboard's chat detail panel, replay, share)
 * where there is no `ElementsProvider` / assistant-ui message context. Fenced
 * code blocks are styled but not syntax-highlighted — the heavier shiki path
 * stays reserved for tool output via `<ToolUI />`.
 */
const MarkdownImpl: FC<MarkdownProps> = ({ children, className }) => {
  return (
    <div className={cn("aui-md", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={markdownComponents}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
};

export const Markdown = memo(MarkdownImpl);

// A fenced/indented code block, as opposed to inline `code`. react-markdown
// (>= v9) no longer passes an `inline` prop, so we infer block status from the
// language class remark attaches and from the presence of newlines.
function isBlockCode(className: string | undefined, text: string): boolean {
  return /language-/.test(className ?? "") || text.includes("\n");
}

// react-markdown passes a code node's text as the children; flatten it to a
// string without risking Object's default "[object Object]" stringification.
function nodeText(node: ReactNode): string {
  if (typeof node === "string") return node;
  if (typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(nodeText).join("");
  return "";
}

const markdownComponents: Components = {
  h1: ({ className, ...props }) => (
    <h1
      className={cn(
        "aui-md-h1 mb-8 scroll-m-20 text-4xl font-extrabold tracking-tight last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  h2: ({ className, ...props }) => (
    <h2
      className={cn(
        "aui-md-h2 mt-8 mb-4 scroll-m-20 text-3xl font-semibold tracking-tight first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  h3: ({ className, ...props }) => (
    <h3
      className={cn(
        "aui-md-h3 mt-6 mb-4 scroll-m-20 text-2xl font-semibold tracking-tight first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  h4: ({ className, ...props }) => (
    <h4
      className={cn(
        "aui-md-h4 mt-6 mb-4 scroll-m-20 text-xl font-semibold tracking-tight first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  h5: ({ className, ...props }) => (
    <h5
      className={cn(
        "aui-md-h5 my-4 text-lg font-semibold first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  h6: ({ className, ...props }) => (
    <h6
      className={cn(
        "aui-md-h6 my-4 font-semibold first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  p: ({ className, ...props }) => (
    <p
      className={cn(
        "aui-md-p mt-5 mb-5 leading-7 first:mt-0 last:mb-0",
        className,
      )}
      {...props}
    />
  ),
  a: ({ className, ...props }) => (
    <a
      className={cn(
        "aui-md-a font-medium text-primary underline underline-offset-4",
        className,
      )}
      {...props}
    />
  ),
  blockquote: ({ className, ...props }) => (
    <blockquote
      className={cn("aui-md-blockquote border-l-2 pl-6 italic", className)}
      {...props}
    />
  ),
  ul: ({ className, ...props }) => (
    <ul
      className={cn("aui-md-ul my-5 ml-6 list-disc [&>li]:mt-2", className)}
      {...props}
    />
  ),
  ol: ({ className, ...props }) => (
    <ol
      className={cn("aui-md-ol my-5 ml-6 list-decimal [&>li]:mt-2", className)}
      {...props}
    />
  ),
  hr: ({ className, ...props }) => (
    <hr className={cn("aui-md-hr my-5 border-b", className)} {...props} />
  ),
  table: ({ className, ...props }) => (
    <table
      className={cn(
        "aui-md-table my-5 w-full border-separate border-spacing-0 overflow-y-auto",
        className,
      )}
      {...props}
    />
  ),
  th: ({ className, ...props }) => (
    <th
      className={cn(
        "aui-md-th bg-muted px-4 py-2 text-left font-bold first:rounded-tl-lg last:rounded-tr-lg [[align=center]]:text-center [[align=right]]:text-right",
        className,
      )}
      {...props}
    />
  ),
  td: ({ className, ...props }) => (
    <td
      className={cn(
        "aui-md-td border-b border-l px-4 py-2 text-left last:border-r [[align=center]]:text-center [[align=right]]:text-right",
        className,
      )}
      {...props}
    />
  ),
  tr: ({ className, ...props }) => (
    <tr
      className={cn(
        "aui-md-tr m-0 border-b p-0 first:border-t [&:last-child>td:first-child]:rounded-bl-lg [&:last-child>td:last-child]:rounded-br-lg",
        className,
      )}
      {...props}
    />
  ),
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children, ...props }) => {
    const text = nodeText(children).replace(/\n$/, "");
    if (isBlockCode(className, text)) {
      return (
        <pre className="aui-md-pre overflow-x-auto rounded-lg border bg-muted p-4 text-foreground">
          <code className={className} {...props}>
            {text}
          </code>
        </pre>
      );
    }
    return (
      <code
        className={cn(
          "aui-md-inline-code rounded border bg-muted px-1 font-semibold",
          className,
        )}
        {...props}
      >
        {children}
      </code>
    );
  },
};
