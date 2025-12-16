import { cn } from "@/lib/utils";
import Editor, { loader, OnMount } from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import { useEffect, useRef } from "react";

loader.config({ monaco });

interface MonacoEditorProps {
  value: string;
  language: string;
  className?: string;
  readOnly?: boolean;
  height?: string;
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
}: MonacoEditorProps) {
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
      fontSize: 14,
      fontFamily:
        'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
      lineNumbers: "on",
      folding: true,
      automaticLayout: true,
    });
  };

  // Update editor theme when Moonshine theme changes
  useEffect(() => {
    if (editorRef.current) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const monaco = (window as any).monaco;
      if (monaco) {
        monaco.editor.setTheme(theme === "dark" ? "vs-dark" : "vs");
      }
    }
  }, [theme]);

  // Type cast needed for React 19 compatibility with @monaco-editor/react
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const EditorComponent = Editor as any;

  return (
    <div className={cn("border rounded-md overflow-hidden", className)}>
      <EditorComponent
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
          fontSize: 14,
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          lineNumbers: "on",
          folding: true,
          automaticLayout: true,
        }}
        loading={
          <div className="flex items-center justify-center h-full">
            <div className="text-muted-foreground">Loading editor...</div>
          </div>
        }
      />
    </div>
  );
}
