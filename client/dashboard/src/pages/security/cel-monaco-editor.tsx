import { cn } from "@/lib/utils";
import Editor, { loader, type OnMount } from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { useEffect, useRef, type JSX } from "react";

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

// One author-visible variable/function pair, mirrored from the backend's
// getDetectionSchema so completions can't drift from what the engine accepts.
export type CelSchemaItem = {
  name: string;
  detail: string;
  doc: string;
  // Member fields available on each element when this item is a list/object
  // variable (e.g. a `tools` element's name/server/function/args). Used to
  // complete a comprehension's bound variable (tools.exists(t, t.name…)).
  fields?: CelSchemaItem[];
};
// A CEL macro. `member` is true for the list macros invoked after a dot
// (tools.exists(...)); false for the global `has(...)`, which is named at the
// top level like a function call.
export type CelMacroItem = CelSchemaItem & { member: boolean };
export type CelSchema = {
  variables: CelSchemaItem[];
  functions: CelSchemaItem[];
  macros: CelMacroItem[];
};

// Registration state and the active completion schema live on globalThis, not in
// module-level slots. Monaco is a singleton that survives Vite HMR while this
// module is re-evaluated, so module-level state would reset on every hot reload —
// re-running registerCelLanguage and stacking another completion provider on the
// same instance (duplicate suggestions that grow by one per reload). globalThis
// persists across the re-eval, so we register exactly once and the single
// provider always reads the current schema. (It can't live on the Monaco module
// namespace itself — that's a frozen Module object and assigning to it throws.)
interface CelRuntime {
  registered: boolean;
  schema: CelSchema;
}

function celRuntime(): CelRuntime {
  const holder = globalThis as unknown as { __celRuntime?: CelRuntime };
  if (!holder.__celRuntime) {
    holder.__celRuntime = {
      registered: false,
      schema: { variables: [], functions: [], macros: [] },
    };
  }
  return holder.__celRuntime;
}

const FIELD_WORDS = [
  "content",
  "prompt",
  "assistant",
  "output",
  "tools",
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
// List macros CEL exposes on `tools` (and other lists).
// Highlighted as built-in macro/function keywords. Completion offerings come
// from the schema; this list only drives syntax coloring.
const MACRO_WORDS = [
  "has",
  "exists",
  "exists_one",
  "all",
  "filter",
  "map",
  "size",
];

// The resolved type of an access chain, enough to decide what completes after a
// dot: a list offers macros, an object offers its member fields, a field offers
// matchers; anything we can't resolve falls back to matchers (the safe superset
// and the long-standing behaviour).
type CelType =
  | { kind: "list" } // e.g. `tools` — offer comprehension macros
  | { kind: "object"; fields: CelSchemaItem[] } // a bound element — offer fields
  | { kind: "field" } // a matchable field — offer matchers
  | { kind: "unknown" };

// detectBindVariables scans the expression for `<list>.<macro>(<bind>, …)` and
// maps each bound variable name to its element's member fields. Heuristic (no
// real parse, so it can't see nesting or shadowing), but CEL scope expressions
// here are short single statements, so the common `tools.exists(t, …)` resolves.
function detectBindVariables(
  text: string,
  schema: CelSchema,
): Map<string, CelSchemaItem[]> {
  const byName = new Map(schema.variables.map((v) => [v.name, v]));
  const binds = new Map<string, CelSchemaItem[]>();
  const re =
    /([A-Za-z_]\w*)\s*\.\s*(?:exists|exists_one|all|filter|map)\s*\(\s*([A-Za-z_]\w*)\s*,/g;
  for (let mm = re.exec(text); mm; mm = re.exec(text)) {
    const listName = mm[1];
    const bindName = mm[2];
    if (!listName || !bindName) continue;
    const v = byName.get(listName);
    if (v && v.detail.startsWith("list(")) binds.set(bindName, v.fields ?? []);
  }
  return binds;
}

// resolveReceiverType resolves the type of the dotted access chain ending right
// before the cursor — `t` in `t.`, `t.name` in `t.name.` — so the provider can
// offer the right completions for that receiver.
function resolveReceiverType(
  upto: string,
  schema: CelSchema,
  binds: Map<string, CelSchemaItem[]>,
): CelType {
  const m = upto.match(/([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\.\s*\w*$/);
  const raw = m?.[1];
  if (!raw) return { kind: "unknown" };
  const chain = raw.split(".").map((s) => s.trim());
  const byName = new Map(schema.variables.map((v) => [v.name, v]));

  // Head of the chain: a bound element, or a top-level variable.
  const head = chain[0];
  if (!head) return { kind: "unknown" };
  let cur: CelType;
  const boundFields = binds.get(head);
  if (boundFields) {
    cur = { kind: "object", fields: boundFields };
  } else {
    const v = byName.get(head);
    if (!v) return { kind: "unknown" };
    if (v.detail.startsWith("list(")) cur = { kind: "list" };
    else if (v.detail === "field") cur = { kind: "field" };
    else return { kind: "unknown" };
  }

  // Walk member accesses; tool fields are themselves matchable fields.
  for (let i = 1; i < chain.length; i++) {
    const seg = chain[i];
    if (cur.kind !== "object" || seg === undefined) return { kind: "unknown" };
    const f: CelSchemaItem | undefined = cur.fields.find((x) => x.name === seg);
    if (!f) return { kind: "unknown" };
    cur = f.detail === "field" ? { kind: "field" } : { kind: "unknown" };
  }
  return cur;
}

// registerCelLanguage installs the CEL Monarch grammar (syntax highlighting) and
// a schema-driven completion provider. Idempotent per Monaco instance (survives
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
    triggerCharacters: ["."],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      const range: Monaco.IRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      const schema = celRuntime().schema;
      const binds = detectBindVariables(model.getValue(), schema);

      // Text from line start to the cursor — enough lookbehind to tell whether
      // we're after a `.` and, if so, what the receiver of that dot is.
      const upto = model.getValueInRange({
        startLineNumber: 1,
        startColumn: 1,
        endLineNumber: position.lineNumber,
        endColumn: position.column,
      });

      if (/\.\s*\w*$/.test(upto)) {
        const recv = resolveReceiverType(upto, schema, binds);

        // A bound element (`t.`) → its member fields.
        if (recv.kind === "object") {
          return {
            suggestions: recv.fields.map((f) => ({
              label: f.name,
              kind: monaco.languages.CompletionItemKind.Field,
              insertText: f.name,
              detail: f.detail,
              documentation: f.doc,
              range,
            })),
          };
        }

        // A list (`tools.`) → the member macros that iterate it, sourced from
        // the schema so the set can't drift from the engine.
        if (recv.kind === "list") {
          return {
            suggestions: schema.macros
              .filter((m) => m.member)
              .map((m) => ({
                label: m.name,
                kind: monaco.languages.CompletionItemKind.Method,
                insertText: `${m.name}(\${1:t}, $2)`,
                insertTextRules:
                  monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
                detail: m.detail,
                documentation: m.doc,
                range,
              })),
          };
        }

        // A field (or anything unresolved) → the matcher functions.
        return {
          suggestions: schema.functions.map((fn) => ({
            label: fn.name,
            kind: monaco.languages.CompletionItemKind.Method,
            insertText: `${fn.name}($1)`,
            insertTextRules:
              monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
            detail: fn.detail,
            documentation: fn.doc,
            range,
          })),
        };
      }

      // Not after a dot: name a top-level variable or an in-scope bind variable.
      // The list member macros (exists/all/...) are intentionally not offered
      // here — they surface after `tools.` like every other member. Global
      // macros that read like a call (has(...)) do belong at this position.
      const suggestions: Monaco.languages.CompletionItem[] =
        schema.variables.map((v) => ({
          label: v.name,
          kind: monaco.languages.CompletionItemKind.Variable,
          insertText: v.name,
          detail: v.detail,
          documentation: v.doc,
          range,
        }));
      for (const [bindName] of binds) {
        suggestions.push({
          label: bindName,
          kind: monaco.languages.CompletionItemKind.Variable,
          insertText: bindName,
          detail: "bound tool",
          documentation: "Iteration variable bound to a tool call.",
          range,
        });
      }
      for (const m of schema.macros.filter((x) => !x.member)) {
        suggestions.push({
          label: m.name,
          kind: monaco.languages.CompletionItemKind.Method,
          insertText: `${m.name}($1)`,
          insertTextRules:
            monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
          detail: m.detail,
          documentation: m.doc,
          range,
        });
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
  schema,
  errorMessage,
  errorRange,
  disabled,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  schema?: CelSchema;
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

  // Keep the shared completion schema current as the cached query resolves. The
  // single provider reads this same instance slot, so updates apply live.
  useEffect(() => {
    if (schema) celRuntime().schema = schema;
  }, [schema]);

  // Surface the backend compile error as a single inline marker spanning the
  // whole expression (compileCel returns a message, not a position).
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
