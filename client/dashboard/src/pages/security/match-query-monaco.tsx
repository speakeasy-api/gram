import { cn } from "@/lib/utils";
import Editor, {
  loader,
  type BeforeMount,
  type OnChange,
  type OnMount,
} from "@monaco-editor/react";
import { ChevronRight } from "lucide-react";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { Fragment, useRef, type JSX } from "react";
import {
  MATCH_QUERY_EXAMPLES,
  matchQuerySuggestions,
  parseMatchQuery,
} from "./match-query";

loader.config({ monaco });

const LANGUAGE_ID = "matchquery";
const MARKER_OWNER = "matchquery";
let languageRegistered = false;

/** Register the one-line query language (highlighting + completion) once. */
function registerLanguage(m: typeof Monaco) {
  if (languageRegistered) return;
  languageRegistered = true;

  m.languages.register({ id: LANGUAGE_ID });
  m.languages.setMonarchTokensProvider(LANGUAGE_ID, {
    tokenizer: {
      root: [
        [/\b(AND|OR|NOT)\b/, "keyword"],
        [/\/(?:[^/\\]|\\.)*\//, "regexp"],
        [/"(?:[^"\\]|\\.)*"/, "string"],
        [/[a-zA-Z_][\w.$]*(?=\s*:)/, "type"],
        [/[*-]/, "operator"],
        [/[()]/, "@brackets"],
        [/:/, "delimiter"],
      ],
    },
  });

  m.languages.registerCompletionItemProvider(LANGUAGE_ID, {
    triggerCharacters: [":", ".", " ", "-", "("],
    provideCompletionItems(model, position) {
      const value = model.getValue();
      const offset = model.getOffsetAt(position);
      const { from, suggestions } = matchQuerySuggestions(value, offset);
      const start = model.getPositionAt(from);
      const range = new m.Range(
        start.lineNumber,
        start.column,
        position.lineNumber,
        position.column,
      );
      return {
        suggestions: suggestions.map((s) => {
          const snippet = s.caretOffset !== undefined;
          return {
            label: s.label,
            detail: s.description,
            kind: m.languages.CompletionItemKind.Value,
            sortText: String.fromCharCode(97), // keep our ordering
            insertText: snippet
              ? `${s.insert.slice(0, s.caretOffset)}$0${s.insert.slice(s.caretOffset)}`
              : s.insert,
            insertTextRules: snippet
              ? m.languages.CompletionItemInsertTextRule.InsertAsSnippet
              : undefined,
            range,
          };
        }),
      };
    },
  });
}

function updateMarkers(
  m: typeof Monaco,
  model: Monaco.editor.ITextModel | null,
  value: string,
) {
  if (!model) return;
  const { error } = parseMatchQuery(value);
  const markers: Monaco.editor.IMarkerData[] =
    error && value.trim()
      ? [
          {
            severity: m.MarkerSeverity.Error,
            message: error,
            startLineNumber: 1,
            startColumn: 1,
            endLineNumber: 1,
            endColumn: value.length + 1,
          },
        ]
      : [];
  m.editor.setModelMarkers(model, MARKER_OWNER, markers);
}

const EDITOR_OPTIONS: Monaco.editor.IStandaloneEditorConstructionOptions = {
  lineNumbers: "off",
  glyphMargin: false,
  folding: false,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  overviewRulerLanes: 0,
  lineDecorationsWidth: 0,
  lineNumbersMinChars: 0,
  renderLineHighlight: "none",
  wordWrap: "off",
  wrappingStrategy: "advanced",
  scrollbar: {
    vertical: "hidden",
    horizontal: "hidden",
    handleMouseWheel: false,
    alwaysConsumeMouseWheel: false,
  },
  fontFamily: "var(--font-mono, monospace)",
  fontSize: 12,
  automaticLayout: true,
  contextmenu: false,
  fixedOverflowWidgets: true,
  suggestOnTriggerCharacters: true,
  quickSuggestions: { other: true, comments: false, strings: true },
  occurrencesHighlight: "off",
  selectionHighlight: false,
  matchBrackets: "never",
  padding: { top: 9, bottom: 9 },
};

/** Monaco-backed single-line query editor: syntax highlighting, autocomplete,
 *  and inline error markers, all driven by our match-query parser. Drop-in for
 *  MatchQueryInput. */
export function MatchQueryMonaco({
  value,
  onChange,
  error,
}: {
  value: string;
  onChange: (next: string) => void;
  error?: string | null;
}): JSX.Element {
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
  const monacoRef = useRef<typeof Monaco | null>(null);

  const beforeMount: BeforeMount = (m) => registerLanguage(m);

  const onMount: OnMount = (editor, m) => {
    editorRef.current = editor;
    monacoRef.current = m;
    // Single-line: Enter accepts a suggestion instead of inserting a newline.
    editor.addCommand(m.KeyCode.Enter, () => {
      editor.trigger("keyboard", "acceptSelectedSuggestion", {});
    });
    updateMarkers(m, editor.getModel(), value);
  };

  const handleChange: OnChange = (next) => {
    const flat = (next ?? "").replace(/\n/g, " ");
    if (flat !== next) editorRef.current?.setValue(flat);
    onChange(flat);
    const m = monacoRef.current;
    if (m) updateMarkers(m, editorRef.current?.getModel() ?? null, flat);
  };

  return (
    <div className="space-y-2">
      <div
        className={cn(
          "border-input bg-background overflow-hidden rounded-md border",
          error && "border-destructive",
        )}
      >
        <Editor
          height="38px"
          language={LANGUAGE_ID}
          value={value}
          beforeMount={beforeMount}
          onMount={onMount}
          onChange={handleChange}
          options={EDITOR_OPTIONS}
        />
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      <details className="group">
        <summary className="text-muted-foreground hover:text-foreground flex cursor-pointer list-none items-center gap-1 text-xs">
          <ChevronRight className="h-3 w-3 transition-transform group-open:rotate-90" />
          Query syntax & examples
        </summary>
        <div className="border-border bg-muted/30 mt-1.5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-md border p-3">
          {MATCH_QUERY_EXAMPLES.map((ex) => (
            <Fragment key={ex.query}>
              <code className="text-foreground font-mono text-[11px]">
                {ex.query}
              </code>
              <span className="text-muted-foreground text-[11px]">
                {ex.meaning}
              </span>
            </Fragment>
          ))}
        </div>
      </details>
    </div>
  );
}
