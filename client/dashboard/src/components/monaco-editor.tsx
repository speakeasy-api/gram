import { cn } from "@/lib/utils";
import Editor, { loader, OnMount } from "@monaco-editor/react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { useEffect, useRef } from "react";

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

loader.config({ monaco });

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
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);

  const handleEditorDidMount: OnMount = (editor, _monaco) => {
    editorRef.current = editor;

    // Configure editor options for better UX
    editor.updateOptions({
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
    });
  };

  // Update editor theme when Moonshine theme changes
  useEffect(() => {
    if (editorRef.current) {
      monaco.editor.setTheme(theme === "dark" ? "vs-dark" : "vs");
    }
  }, [theme]);

  return (
    <div className={cn("overflow-hidden", className)}>
      <Editor
        height={height}
        language={language}
        value={value}
        theme={theme === "dark" ? "vs-dark" : "vs"}
        onMount={handleEditorDidMount}
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
