import { cn } from "@/lib/utils";
import Editor, { loader } from "@monaco-editor/react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import * as monaco from "monaco-editor";

// oxlint-disable import/default -- Vite ?worker URL imports lack named defaults
import editorWorker from "monaco-editor/editor/editor.worker.js?worker";
import jsonWorker from "monaco-editor/language/json/json.worker.js?worker";
import cssWorker from "monaco-editor/language/css/css.worker.js?worker";
import htmlWorker from "monaco-editor/language/html/html.worker.js?worker";
import tsWorker from "monaco-editor/language/typescript/ts.worker.js?worker";

self.MonacoEnvironment = {
  getWorker(_, label) {
    switch (label) {
      case "json":
        return new jsonWorker();
      case "css":
      case "scss":
      case "less":
        return new cssWorker();
      case "html":
      case "handlebars":
      case "razor":
        return new htmlWorker();
      case "typescript":
      case "javascript":
        return new tsWorker();
      default:
        return new editorWorker();
    }
  },
};

// Point @monaco-editor/react at the bundled monaco. Guarded: a second call after
// init throws, but it's the same instance, so swallow the duplicate. The CEL
// editor on the security pages configures the same loader and carries the
// same guard, so either may load first.
try {
  loader.config({ monaco });
} catch {
  // already configured by another Monaco entry point this session
}

interface MonacoEditorProps {
  value: string;
  language: string;
  className?: string;
  readOnly?: boolean;
  height?: string;
  wordWrap?: "on" | "off" | "wordWrapColumn" | "bounded";
}

/**
 * MonacoEditor component with theme integration and virtual scrolling.
 *
 * This component uses Monaco Editor (the same editor as VS Code) which provides
 * excellent performance for large files through virtual scrolling - only visible
 * lines are rendered, making it handle files with tens of thousands of lines smoothly.
 */
export function MonacoEditor({
  value,
  language,
  className,
  readOnly = true,
  height = "100%",
  wordWrap = "off",
}: MonacoEditorProps): JSX.Element {
  const { theme } = useMoonshineConfig();

  return (
    <div className={cn("overflow-hidden", className)}>
      <Editor
        height={height}
        language={language}
        value={value}
        theme={theme === "dark" ? "vs-dark" : "vs"}
        options={{
          readOnly,
          minimap: { enabled: true },
          scrollBeyondLastLine: false,
          renderWhitespace: "selection",
          fontSize: 12,
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          lineNumbers: "on",
          folding: true,
          automaticLayout: true,
          wordWrap,
        }}
        loading={
          <div className="flex h-full items-center justify-center">
            <div className="text-muted-foreground">Loading editor...</div>
          </div>
        }
      />
    </div>
  );
}
