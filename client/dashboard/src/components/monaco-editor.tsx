import { cn } from "@/lib/utils";
import Editor, { DiffEditor, loader, OnMount } from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { useEffect, useRef } from "react";

import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import cssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import htmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import tsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";

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

interface CorpusEditorProps {
  value?: string;
  path: string;
  language?: string;
  className?: string;
  readOnly?: boolean;
  height?: string;
  onChange?: (value: string) => void;
  onValidate?: (markers: Monaco.editor.IMarker[]) => void;
  options?: Monaco.editor.IStandaloneEditorConstructionOptions;
}

interface CorpusDiffEditorProps {
  original?: string;
  modified?: string;
  path: string;
  language?: string;
  className?: string;
  readOnly?: boolean;
  height?: string;
  onChange?: (value: string) => void;
  options?: Monaco.editor.IDiffEditorConstructionOptions;
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
    <div className={cn("overflow-hidden", className)}>
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
          fontSize: 12,
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          lineNumbers: "on",
          folding: true,
          automaticLayout: true,
          wordWrap,
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

export function CorpusEditor({
  value,
  path,
  language = "markdown",
  className,
  readOnly = false,
  height = "100%",
  onChange,
  onValidate,
  options,
}: CorpusEditorProps) {
  const { theme } = useMoonshineConfig();
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
  const themeName = theme === "dark" ? "vs-dark" : "vs";

  const handleBeforeMount = (_monaco: typeof Monaco) => {};

  const handleEditorMount: OnMount = (editor, _monaco) => {
    editorRef.current = editor;
  };

  const handleMarkersChanged = (markers: Monaco.editor.IMarker[]) => {
    onValidate?.(markers);
  };

  // Type cast needed for React 19 compatibility with @monaco-editor/react
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const MonacoCodeEditor = Editor as any;

  return (
    <div className={cn("overflow-hidden", className)} style={{ height }}>
      <MonacoCodeEditor
        value={value ?? ""}
        height="100%"
        key={path}
        beforeMount={handleBeforeMount}
        onMount={handleEditorMount}
        language={language}
        onChange={(nextValue: string | undefined) =>
          onChange?.(nextValue ?? "")
        }
        theme={themeName}
        wrapperProps={{
          className: cn("flex-1 overflow-auto"),
        }}
        onValidate={handleMarkersChanged}
        options={{
          fixedOverflowWidgets: true,
          padding: { top: 30, bottom: 30 },
          renderValidationDecorations: "on",
          overviewRulerLanes: 0,
          wrappingStrategy: "simple",
          wordWrap: "on",
          links: false,
          contextmenu: false,
          scrollBeyondLastLine: false,
          scrollBeyondLastColumn: 0,
          formatOnPaste: true,
          formatOnType: true,
          wordBasedSuggestions: false,
          largeFileOptimizations: true,
          automaticLayout: true,
          minimap: { enabled: false },
          fontSize: 12,
          readOnly,
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          ...options,
        }}
        path={path}
        loading={
          <div className="flex items-center justify-center h-full">
            <div className="text-muted-foreground">Loading editor...</div>
          </div>
        }
      />
    </div>
  );
}

export function CorpusDiffEditor({
  original,
  modified,
  path,
  language = "markdown",
  className,
  readOnly = false,
  height = "100%",
  onChange,
  options,
}: CorpusDiffEditorProps) {
  const { theme } = useMoonshineConfig();
  const themeName = theme === "dark" ? "vs-dark" : "vs";
  const diffEditorRef = useRef<Monaco.editor.IStandaloneDiffEditor | null>(
    null,
  );
  const changeSubscriptionRef = useRef<Monaco.IDisposable | null>(null);

  useEffect(() => {
    return () => {
      changeSubscriptionRef.current?.dispose();
      changeSubscriptionRef.current = null;
    };
  }, []);

  const handleDiffMount = (editor: Monaco.editor.IStandaloneDiffEditor) => {
    diffEditorRef.current = editor;

    changeSubscriptionRef.current?.dispose();
    changeSubscriptionRef.current = null;

    const modifiedEditor = editor.getModifiedEditor();
    changeSubscriptionRef.current = modifiedEditor.onDidChangeModelContent(
      () => {
        onChange?.(modifiedEditor.getValue());
      },
    );
  };

  // Type cast needed for React 19 compatibility with @monaco-editor/react
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const MonacoDiffEditor = DiffEditor as any;

  return (
    <div className={cn("overflow-hidden", className)} style={{ height }}>
      <MonacoDiffEditor
        key={path}
        original={original ?? ""}
        modified={modified ?? ""}
        height="100%"
        language={language}
        originalModelPath={`${path}:original`}
        modifiedModelPath={`${path}:modified`}
        keepCurrentOriginalModel
        keepCurrentModifiedModel
        theme={themeName}
        wrapperProps={{
          className: cn("flex-1 overflow-auto"),
        }}
        onMount={handleDiffMount}
        options={{
          automaticLayout: true,
          fixedOverflowWidgets: true,
          fontSize: 12,
          minimap: { enabled: false },
          readOnly,
          renderGutterMenu: false,
          renderMarginRevertIcon: false,
          renderSideBySide: false,
          scrollBeyondLastLine: false,
          wordWrap: "on",
          ...options,
        }}
        loading={
          <div className="flex items-center justify-center h-full">
            <div className="text-muted-foreground">Loading diff...</div>
          </div>
        }
      />
    </div>
  );
}
