import { cn } from "@/lib/utils";
import Editor, { loader, type OnMount } from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { useEffect, useRef, type JSX } from "react";
import type { CelCompletionItem, CelEngine } from "./cel-wasm";

// oxlint-disable-next-line import/default -- Vite ?worker URL imports lack named defaults
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";

// CEL has no dedicated language worker; the base editor worker drives our custom
// Monarch language. Only set the environment if the app hasn't already (the
// shared MonacoEditor configures the full worker set when it's on the page).
if (!self.MonacoEnvironment) {
  self.MonacoEnvironment = { getWorker: () => new editorWorker() };
}
// Point @monaco-editor/react at the bundled monaco instead of the CDN. Guarded:
// the shared MonacoEditor (components/monaco-editor.tsx) may have already called
// this on another route, and a second call after monaco has initialized throws —
// it's the same bundled instance either way, so swallow the duplicate.
try {
  loader.config({ monaco });
} catch {
  // already configured by another Monaco entry point this session
}

const CEL_LANGUAGE_ID = "cel";

// Registration state and the active engine live on globalThis, not in module-
// level slots. Monaco is a singleton that survives Vite HMR while this module is
// re-evaluated, so module-level state would reset on every hot reload — re-
// running registerCelLanguage and stacking another completion provider on the
// same instance (duplicate suggestions that grow by one per reload). globalThis
// persists across the re-eval, so we register exactly once and the single
// provider always reads the current engine. (It can't live on the Monaco module
// namespace itself — that's a frozen Module object and assigning to it throws.)
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
// List macros CEL exposes on `tool_calls` (and other lists).
// Highlighted as built-in macro/function keywords. Completion offerings come
// from the engine; this list only drives syntax coloring.
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

// suggestionFor turns one engine completion item into a Monaco suggestion. The
// engine decided WHAT belongs here (type-directed); this only maps category to
// an icon and an insertion snippet — present() takes no args, get/match* take
// one, the comprehension macros take a bound var and a body.
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

// registerCelLanguage installs the CEL Monarch grammar (syntax highlighting) and
// an engine-driven completion provider. Idempotent per Monaco instance (survives
// HMR), so it registers the language exactly once.
function registerCelLanguage(m: typeof Monaco): void {
  const rt = celRuntime();
  if (rt.registered) return;
  rt.registered = true;

  m.languages.register({ id: CEL_LANGUAGE_ID });

  // Transparent-background themes so the editor blends into the surrounding
  // input chrome (the container paints `dark:bg-input/30`) instead of showing
  // Monaco's opaque IDE background, which reads as a foreign element.
  // focusBorder is transparent so Monaco doesn't paint its default blue focus
  // ring inside our card; the bordered container is the only frame we want.
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

      // No engine yet (wasm still loading, or unavailable): offer nothing rather
      // than stale guesses — validation falls back to the server on save.
      if (!engine) return { suggestions: [] };

      // The engine resolves the receiver's real type and returns exactly what's
      // valid here — fields after a tool, macros after a list, matchers after a
      // field. No receiver-type heuristics on this side.
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

// Register the language at module load — `monaco` here is the same instance the
// Editor uses (via loader.config above), so the grammar + completions exist
// before any editor mounts and there's no plaintext-highlight flash.
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

  // Publish the engine to the single shared completion provider as it loads. The
  // provider reads this same slot, so completion turns on the moment wasm is ready.
  useEffect(() => {
    celRuntime().engine = engine ?? null;
  }, [engine]);

  // Surface the backend compile error as a single inline marker spanning the
  // whole expression (compileExpr returns a message, not a position).
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

    // Expand the suggestion details panel (matcher signature + description) by
    // default so authors see what a field/matcher does without first clicking
    // the widget's chevron. Monaco has no editor option for this — the suggest
    // widget reads the global `expandSuggestionDocs` storage flag on every open
    // — so we flip it through the suggest controller's widget. Touching
    // `widget.value` eagerly creates the (otherwise lazy) widget; guarded since
    // it's a private API that could drift across a Monaco upgrade.
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

    // Open the completion list as soon as the field is focused so the available
    // fields/matchers are discoverable without typing first. Deferred a tick so
    // the trigger runs after focus + cursor placement have settled.
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
          // Render the suggest/hover widgets in a fixed overflow layer so they
          // escape this card's `overflow-hidden` instead of being clipped inside.
          fixedOverflowWidgets: true,
          // We supply a curated schema; Monaco's word-based completions only add
          // noise (duplicate bare identifiers pulled from other editor models).
          wordBasedSuggestions: "off",
        }}
      />
    </div>
  );
}
