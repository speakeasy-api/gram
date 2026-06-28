import { cn } from "@/lib/utils";
import Editor, { loader, type OnMount } from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { useEffect, useRef, type JSX } from "react";
import type { CelCompletionItem, CelEngine } from "./cel-wasm";

// oxlint-disable-next-line import/default -- Vite ?worker URL imports lack named defaults
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";

// CEL has no dedicated worker; the base editor worker drives our Monarch
// language. Only set the environment if the shared MonacoEditor hasn't already.
if (!self.MonacoEnvironment) {
  self.MonacoEnvironment = { getWorker: () => new editorWorker() };
}
// Point @monaco-editor/react at the bundled monaco. Guarded: a second call after
// init throws, but it's the same instance, so swallow the duplicate.
try {
  loader.config({ monaco });
} catch {
  // already configured by another Monaco entry point this session
}

const CEL_LANGUAGE_ID = "cel";

// Registration state + active engine live on globalThis so they survive Vite HMR
// (module-level state would reset and stack duplicate completion providers).
interface CelRuntime {
  registered: boolean;
  engine: CelEngine | null;
}

function celRuntime(): CelRuntime {
  const holder = globalThis as unknown as { __celRuntime?: CelRuntime };
  if (!holder.__celRuntime) {
    holder.__celRuntime = { registered: false, engine: null };
  }
  return holder.__celRuntime;
}

const FIELD_WORDS = [
  "content",
  "prompt",
  "assistant",
  "tool_result",
  "tool_calls",
  "type",
];
const MATCHER_WORDS = [
  "matchRegex",
  "matchText",
  "matchExact",
  "matchPrefix",
  "matchSuffix",
  "matchGlob",
  "present",
  "get",
];
// List macros, for syntax coloring only (completions come from the engine).
const MACRO_WORDS = [
  "has",
  "exists",
  "exists_one",
  "all",
  "filter",
  "map",
  "size",
];

function shouldSuggestLogicalOperators(upto: string): boolean {
  const trimmed = upto.trimEnd();
  if (!trimmed) return false;
  if (/[&|]$/.test(trimmed)) return true;
  return /(?:\)|\]|\}|"|'|[A-Za-z_]\w*)$/.test(trimmed);
}

// suggestionFor maps an engine completion item's category to a Monaco icon and
// insert snippet (the engine already decided what belongs here).
function suggestionFor(
  item: CelCompletionItem,
  range: Monaco.IRange,
): Monaco.languages.CompletionItem {
  const snippet = (text: string) => ({
    insertText: text,
    insertTextRules:
      monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
  });
  const base = {
    label: item.label,
    detail: item.detail,
    documentation: item.doc,
    range,
  };
  switch (item.category) {
    case "field":
      return {
        ...base,
        kind: monaco.languages.CompletionItemKind.Field,
        insertText: item.label,
      };
    case "variable":
    case "bind":
      return {
        ...base,
        kind: monaco.languages.CompletionItemKind.Variable,
        insertText: item.label,
      };
    case "macro": // list comprehension macro: exists(t, …)
      return {
        ...base,
        kind: monaco.languages.CompletionItemKind.Method,
        ...snippet(`${item.label}(\${1:t}, $2)`),
      };
    case "matcher":
    case "globalMacro":
    default:
      // present() is the only nullary matcher; everything else takes one arg.
      return {
        ...base,
        kind: monaco.languages.CompletionItemKind.Method,
        ...(item.label === "present"
          ? { insertText: "present()" }
          : snippet(`${item.label}($1)`)),
      };
  }
}

// registerCelLanguage installs the Monarch grammar and the completion provider,
// once per Monaco instance (survives HMR).
function registerCelLanguage(m: typeof Monaco): void {
  const rt = celRuntime();
  if (rt.registered) return;
  rt.registered = true;

  m.languages.register({ id: CEL_LANGUAGE_ID });

  // Transparent background + focus ring so the editor blends into the input
  // chrome instead of showing Monaco's opaque IDE frame.
  m.editor.defineTheme("cel-dark", {
    base: "vs-dark",
    inherit: true,
    rules: [],
    colors: { "editor.background": "#00000000", focusBorder: "#00000000" },
  });
  m.editor.defineTheme("cel-light", {
    base: "vs",
    inherit: true,
    rules: [],
    colors: { "editor.background": "#00000000", focusBorder: "#00000000" },
  });

  m.languages.setMonarchTokensProvider(CEL_LANGUAGE_ID, {
    keywords: ["true", "false", "null", "in"],
    fields: FIELD_WORDS,
    matchers: MATCHER_WORDS,
    macros: MACRO_WORDS,
    tokenizer: {
      root: [
        [
          /[a-zA-Z_]\w*/,
          {
            cases: {
              "@keywords": "keyword",
              "@fields": "variable.predefined",
              "@matchers": "type.identifier",
              "@macros": "keyword.control",
              "@default": "identifier",
            },
          },
        ],
        [/\s+/, "white"],
        [/"([^"\\]|\\.)*"/, "string"],
        [/'([^'\\]|\\.)*'/, "string"],
        [/\d+/, "number"],
        [/[{}()[\]]/, "@brackets"],
        [/&&|\|\||==|!=|<=|>=|[!<>+\-*/%?:.]/, "operator"],
        [/[,;]/, "delimiter"],
      ],
    },
  });

  m.languages.setLanguageConfiguration(CEL_LANGUAGE_ID, {
    brackets: [
      ["(", ")"],
      ["[", "]"],
      ["{", "}"],
    ],
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: "[", close: "]" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    surroundingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  });

  m.languages.registerCompletionItemProvider(CEL_LANGUAGE_ID, {
    triggerCharacters: [".", "&", "|"],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      const range: Monaco.IRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      const engine = celRuntime().engine;

      // Text from the expression start to the cursor — the engine reads this to
      // resolve the receiver's type and decide what's valid here.
      const upto = model.getValueInRange({
        startLineNumber: 1,
        startColumn: 1,
        endLineNumber: position.lineNumber,
        endColumn: position.column,
      });
      const operatorPrefix = upto.match(/[&|]$/)?.[0];
      const operatorRange: Monaco.IRange = operatorPrefix
        ? {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: position.column - 1,
            endColumn: position.column,
          }
        : range;

      if (operatorPrefix) {
        const label = operatorPrefix === "&" ? "&&" : "||";
        return {
          suggestions: [
            {
              label,
              kind: monaco.languages.CompletionItemKind.Operator,
              insertText: `${label} `,
              detail: "logical operator",
              range: operatorRange,
            },
          ],
        };
      }

      // No engine yet (wasm loading/unavailable): offer nothing.
      if (!engine) return { suggestions: [] };

      // The engine resolves the receiver's type and returns what's valid here.
      const completion = engine.complete(upto);
      const suggestions = completion.items.map((item) =>
        suggestionFor(item, range),
      );

      // Logical operators aren't part of the engine's vocabulary; offer them at a
      // name position once there's a complete operand to join onto.
      if (
        completion.context === "name" &&
        shouldSuggestLogicalOperators(upto)
      ) {
        suggestions.push(
          {
            label: "&&",
            kind: monaco.languages.CompletionItemKind.Operator,
            insertText: "&& ",
            detail: "logical and",
            range: operatorRange,
          },
          {
            label: "||",
            kind: monaco.languages.CompletionItemKind.Operator,
            insertText: "|| ",
            detail: "logical or",
            range: operatorRange,
          },
        );
      }
      return { suggestions };
    },
  });
}

// Register at module load so the grammar exists before any editor mounts.
registerCelLanguage(monaco);

/** A controlled Monaco editor for a single CEL expression: Monarch syntax
 *  highlighting, schema-driven autocomplete, and an inline error marker driven
 *  by the debounced backend compile (errorMessage). Chrome is stripped so it
 *  reads as a compact code field, not a full IDE pane. */
export function CelMonacoEditor({
  value,
  onChange,
  engine,
  errorMessage,
  errorRange,
  disabled,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  engine?: CelEngine | null;
  errorMessage?: string | null;
  // Character offset range of the error within the expression; when present the
  // marker underlines just that span instead of the whole expression.
  errorRange?: { start: number; end: number } | null;
  disabled?: boolean;
  className?: string;
}): JSX.Element {
  const { theme } = useMoonshineConfig();
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
  const monacoRef = useRef<typeof Monaco | null>(null);

  // Publish the engine to the shared completion provider as it loads.
  useEffect(() => {
    celRuntime().engine = engine ?? null;
  }, [engine]);

  // Surface the compile error as an inline marker.
  useEffect(() => {
    const m = monacoRef.current;
    const editor = editorRef.current;
    if (!m || !editor) return;
    const model = editor.getModel();
    if (!model) return;
    if (errorMessage) {
      // Underline the offending span when the checker gave us a range; else fall
      // back to the whole expression.
      let start = { lineNumber: 1, column: 1 };
      const lastLine = model.getLineCount();
      let end = {
        lineNumber: lastLine,
        column: model.getLineMaxColumn(lastLine),
      };
      if (errorRange && errorRange.end > errorRange.start) {
        start = model.getPositionAt(errorRange.start);
        end = model.getPositionAt(errorRange.end);
      }
      m.editor.setModelMarkers(model, CEL_LANGUAGE_ID, [
        {
          severity: m.MarkerSeverity.Error,
          message: errorMessage,
          startLineNumber: start.lineNumber,
          startColumn: start.column,
          endLineNumber: end.lineNumber,
          endColumn: end.column,
        },
      ]);
    } else {
      m.editor.setModelMarkers(model, CEL_LANGUAGE_ID, []);
    }
  }, [errorMessage, errorRange, value]);

  const handleMount: OnMount = (editor, m) => {
    editorRef.current = editor;
    monacoRef.current = m;
    editor.updateOptions({ readOnly: disabled });

    // Expand the suggestion details panel by default. No editor option exists, so
    // flip it via the suggest controller's widget — a private API, hence guarded.
    try {
      const suggest = editor.getContribution(
        "editor.contrib.suggestController",
      ) as unknown as {
        widget?: { value?: { _setDetailsVisible?: (v: boolean) => void } };
      } | null;
      suggest?.widget?.value?._setDetailsVisible?.(true);
    } catch {
      // Private suggest API changed; details just stay collapsed until toggled.
    }

    // Open the completion list on focus so fields/matchers are discoverable
    // without typing first (deferred a tick, after focus + cursor settle).
    editor.onDidFocusEditorText(() => {
      setTimeout(() => {
        editor.trigger("focus", "editor.action.triggerSuggest", {});
      }, 0);
    });
  };

  return (
    <div
      className={cn(
        "border-input dark:bg-input/30 w-full overflow-hidden rounded-md border bg-transparent py-2 shadow-xs",
        className,
      )}
    >
      <Editor
        language={CEL_LANGUAGE_ID}
        value={value}
        theme={theme === "dark" ? "cel-dark" : "cel-light"}
        onMount={handleMount}
        onChange={(v) => onChange(v ?? "")}
        height="64px"
        options={{
          readOnly: disabled,
          minimap: { enabled: false },
          lineNumbers: "off",
          folding: false,
          glyphMargin: false,
          // Doubles as the editor's left padding (no line-number gutter) so text
          // doesn't hug the border, matching the px-3 of a standard input.
          lineDecorationsWidth: 12,
          lineNumbersMinChars: 0,
          overviewRulerLanes: 0,
          hideCursorInOverviewRuler: true,
          scrollBeyondLastLine: false,
          scrollbar: { vertical: "auto", horizontal: "hidden" },
          wordWrap: "on",
          fontSize: 14,
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
          automaticLayout: true,
          contextmenu: false,
          renderLineHighlight: "none",
          padding: { top: 2, bottom: 2 },
          suggestOnTriggerCharacters: true,
          quickSuggestions: true,
          // Fixed overflow layer so suggest/hover widgets escape the card's
          // overflow-hidden.
          fixedOverflowWidgets: true,
          // We supply a curated schema; Monaco's word-based completions only add
          // noise (duplicate bare identifiers pulled from other editor models).
          wordBasedSuggestions: "off",
        }}
      />
    </div>
  );
}
