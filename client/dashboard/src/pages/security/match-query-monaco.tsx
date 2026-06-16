import { cn } from "@/lib/utils";
import Editor, {
  loader,
  type BeforeMount,
  type OnChange,
  type OnMount,
} from "@monaco-editor/react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { ChevronRight } from "lucide-react";
import type * as Monaco from "monaco-editor";
import * as monaco from "monaco-editor";
import { Fragment, useEffect, useRef, type JSX } from "react";
import {
  MATCH_QUERY_EXAMPLES,
  matchQuerySuggestions,
  parseMatchQuery,
} from "./match-query";

loader.config({ monaco });

const THEME_LIGHT = "matchquery-light";
const THEME_DARK = "matchquery-dark";

/** Transparent-background themes so the editor blends into the form input,
 *  with token colors for fields / keywords / regex / strings. */
function defineThemes(m: typeof Monaco) {
  const transparent = {
    "editor.background": "#00000000",
    focusBorder: "#00000000",
    "editor.lineHighlightBackground": "#00000000",
    "editor.lineHighlightBorder": "#00000000",
    "editor.selectionHighlightBackground": "#00000000",
    "editor.selectionHighlightBorder": "#00000000",
    "editor.wordHighlightBackground": "#00000000",
    "editor.wordHighlightStrongBackground": "#00000000",
    "editor.wordHighlightBorder": "#00000000",
    "editor.wordHighlightStrongBorder": "#00000000",
    "editor.rangeHighlightBackground": "#00000000",
    "editor.rangeHighlightBorder": "#00000000",
    "editor.findMatchHighlightBackground": "#00000000",
    "editorBracketMatch.background": "#00000000",
    "editorBracketMatch.border": "#00000000",
    "editorIndentGuide.background": "#00000000",
  };
  // The editor blends into the input (transparent), but the suggest widget is a
  // separate popover — theme it to match the dashboard's surface/border so the
  // dropdown reads as part of the app, not stock Monaco.
  const darkColors = {
    ...transparent,
    "editorSuggestWidget.background": "#18181b",
    "editorSuggestWidget.border": "#27272a",
    "editorSuggestWidget.foreground": "#e4e4e7",
    "editorSuggestWidget.selectedBackground": "#27272a",
    "editorSuggestWidget.selectedForeground": "#fafafa",
    "editorSuggestWidget.highlightForeground": "#60a5fa",
    "editorSuggestWidget.focusHighlightForeground": "#93c5fd",
  };
  const lightColors = {
    ...transparent,
    "editorSuggestWidget.background": "#ffffff",
    "editorSuggestWidget.border": "#e4e4e7",
    "editorSuggestWidget.foreground": "#18181b",
    "editorSuggestWidget.selectedBackground": "#f4f4f5",
    "editorSuggestWidget.selectedForeground": "#18181b",
    "editorSuggestWidget.highlightForeground": "#2563eb",
    "editorSuggestWidget.focusHighlightForeground": "#1d4ed8",
  };
  m.editor.defineTheme(THEME_DARK, {
    base: "vs-dark",
    inherit: true,
    rules: [
      { token: "type", foreground: "4ec9b0" },
      { token: "keyword", foreground: "c586c0" },
      { token: "regexp", foreground: "d16969" },
      { token: "string", foreground: "ce9178" },
      { token: "operator", foreground: "9aa0a6" },
      { token: "delimiter", foreground: "9aa0a6" },
    ],
    colors: darkColors,
  });
  m.editor.defineTheme(THEME_LIGHT, {
    base: "vs",
    inherit: true,
    rules: [
      { token: "type", foreground: "267f99" },
      { token: "keyword", foreground: "af00db" },
      { token: "regexp", foreground: "b91c1c" },
      { token: "string", foreground: "a31515" },
      { token: "operator", foreground: "6b7280" },
      { token: "delimiter", foreground: "6b7280" },
    ],
    colors: lightColors,
  });
}

const LANGUAGE_ID = "matchquery";
const MARKER_OWNER = "matchquery";

/** Register the one-line query language (highlighting + completion) once.
 *  Guarded on Monaco's own registry (not a module flag) so a Vite HMR reload —
 *  which re-evaluates this module but leaves the prior provider registered —
 *  doesn't add a second completion provider and duplicate every suggestion. */
function registerLanguage(m: typeof Monaco) {
  if (m.languages.getLanguages().some((l) => l.id === LANGUAGE_ID)) return;

  defineThemes(m);
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
    triggerCharacters: [":", ".", " ", "-"],
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
            // Group rides in the label's right-aligned slot (never truncated);
            // the full description goes to documentation (wrapping flyout) so it
            // isn't clipped like `detail` is in the narrow suggest widget.
            label: { label: s.label, description: s.group },
            documentation: { value: s.description },
            kind: m.languages.CompletionItemKind.Value,
            sortText: String.fromCharCode(97), // keep our ordering
            insertText: snippet
              ? `${s.insert.slice(0, s.caretOffset)}$0${s.insert.slice(s.caretOffset)}`
              : s.insert,
            insertTextRules: snippet
              ? m.languages.CompletionItemInsertTextRule.InsertAsSnippet
              : undefined,
            // Re-open suggestions after accepting (field → values, AND → field)
            // so chaining flows without re-typing a trigger character.
            command: snippet
              ? undefined
              : { id: "editor.action.triggerSuggest", title: "" },
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
  fontFamily:
    'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
  fontSize: 12,
  lineHeight: 20,
  automaticLayout: true,
  contextmenu: false,
  fixedOverflowWidgets: true,
  suggestOnTriggerCharacters: true,
  acceptSuggestionOnEnter: "on",
  // Always pre-select the first item so Enter has something to accept.
  suggestSelection: "first",
  // Match the dropdown's text to the editor's (same mono font + size).
  suggestFontSize: 12,
  suggestLineHeight: 20,
  quickSuggestions: { other: true, comments: false, strings: true },
  // Our completion provider is the only source — Monaco's built-in word
  // completions would otherwise duplicate every suggestion.
  wordBasedSuggestions: "off",
  occurrencesHighlight: "off",
  selectionHighlight: false,
  renderValidationDecorations: "on",
  matchBrackets: "never",
  padding: { top: 0, bottom: 0 },
  suggest: { showStatusBar: false, showIcons: false },
  guides: { indentation: false },
};

// Minimal shape of Monaco's (internal) suggest controller — just enough to
// expand the documentation flyout. Guarded with optional chaining so a Monaco
// upgrade that moves these around degrades to "no auto-docs", not a crash.
interface SuggestControllerLike {
  widget?: { value?: { onDidShow?: (cb: () => void) => Monaco.IDisposable } };
  toggleSuggestionDetails?: () => void;
}

// The doc flyout's expanded/collapsed state lives in a profile-scoped store that
// is in-memory (and shared) for standalone editors, so it resets to collapsed on
// every page load. Prime it true on the first widget show across the page; after
// that Monaco auto-expands every popup itself.
let docsExpansionPrimed = false;

/** Monaco-backed single-line query editor: syntax highlighting, autocomplete,
 *  and inline error markers, all driven by our match-query parser. Drop-in for
 *  MatchQueryInput. */
export function MatchQueryMonaco({
  value,
  onChange,
  error,
  showExamples = true,
}: {
  value: string;
  onChange: (next: string) => void;
  error?: string | null;
  /** Render the "Query syntax & examples" affordance below the editor. Turn off
   *  when several editors share a single legend (e.g. the scope/exempt pair). */
  showExamples?: boolean;
}): JSX.Element {
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
  const monacoRef = useRef<typeof Monaco | null>(null);
  const { theme } = useMoonshineConfig();
  const monacoTheme = theme === "dark" ? THEME_DARK : THEME_LIGHT;

  const beforeMount: BeforeMount = (m) => registerLanguage(m);

  const onMount: OnMount = (editor, m) => {
    editorRef.current = editor;
    monacoRef.current = m;
    // Enter accepts the highlighted suggestion via Monaco's native
    // acceptSuggestionOnEnter when the widget is open. When it's closed, swallow
    // Enter so a single-line query never gains a newline.
    editor.addAction({
      id: "matchquery.swallowEnter",
      label: "Swallow Enter",
      keybindings: [m.KeyCode.Enter],
      precondition: "!suggestWidgetVisible",
      run: () => {},
    });
    // Open the field suggestions immediately when focusing an empty query.
    editor.onDidFocusEditorText(() => {
      if (!editor.getValue().trim()) {
        editor.trigger("focus", "editor.action.triggerSuggest", {});
      }
    });
    // Auto-open the documentation flyout so the field/operator descriptions are
    // visible without a manual toggle (see docsExpansionPrimed).
    const suggest = editor.getContribution<Monaco.editor.IEditorContribution>(
      "editor.contrib.suggestController",
    ) as unknown as SuggestControllerLike | null;
    const widget = suggest?.widget?.value;
    if (widget?.onDidShow && suggest?.toggleSuggestionDetails) {
      const sub = widget.onDidShow(() => {
        sub.dispose();
        if (docsExpansionPrimed) return;
        docsExpansionPrimed = true;
        suggest.toggleSuggestionDetails?.();
      });
    }
    updateMarkers(m, editor.getModel(), value);
  };

  // Keep Monaco's theme in sync with the dashboard light/dark mode.
  useEffect(() => {
    monacoRef.current?.editor.setTheme(monacoTheme);
  }, [monacoTheme]);

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
          "border-input bg-background focus-within:ring-ring flex h-10 items-center overflow-hidden rounded-md border pl-2.5 focus-within:ring-1",
          error && "border-destructive",
        )}
      >
        <Editor
          className="w-full"
          height="22px"
          language={LANGUAGE_ID}
          theme={monacoTheme}
          value={value}
          beforeMount={beforeMount}
          onMount={onMount}
          onChange={handleChange}
          options={EDITOR_OPTIONS}
        />
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      {showExamples && <MatchQueryExamples />}
    </div>
  );
}

// Token colors mirror the editor themes (defineThemes) so the static examples
// highlight identically to what's typed in the editor.
const QUERY_TOKEN_COLORS = {
  light: {
    type: "#267f99",
    keyword: "#af00db",
    regexp: "#b91c1c",
    string: "#a31515",
    operator: "#6b7280",
  },
  dark: {
    type: "#4ec9b0",
    keyword: "#c586c0",
    regexp: "#d16969",
    string: "#ce9178",
    operator: "#9aa0a6",
  },
} as const;

// Mirrors the Monarch tokenizer: keyword | regex | string | field (before ':')
// | operator/delimiter.
const QUERY_TOKEN_RE =
  /(\b(?:AND|OR|NOT)\b)|(\/(?:[^/\\]|\\.)*\/)|("(?:[^"\\]|\\.)*")|([a-zA-Z_][\w.$]*(?=\s*:))|([*\-:()])/g;

/** Renders a match-query string with the editor's token colors. */
function HighlightedQuery({ query }: { query: string }): JSX.Element {
  const { theme } = useMoonshineConfig();
  const colors =
    theme === "dark" ? QUERY_TOKEN_COLORS.dark : QUERY_TOKEN_COLORS.light;
  const parts: (string | JSX.Element)[] = [];
  let last = 0;
  for (const m of query.matchAll(QUERY_TOKEN_RE)) {
    const idx = m.index ?? 0;
    if (idx > last) parts.push(query.slice(last, idx));
    let color: string = colors.operator;
    if (m[1]) color = colors.keyword;
    else if (m[2]) color = colors.regexp;
    else if (m[3]) color = colors.string;
    else if (m[4]) color = colors.type;
    parts.push(
      <span key={idx} style={{ color }}>
        {m[0]}
      </span>,
    );
    last = idx + m[0].length;
  }
  if (last < query.length) parts.push(query.slice(last));
  return <code className="font-mono text-[11px]">{parts}</code>;
}

/** The "Query syntax & examples" affordance: a one-line operator legend plus
 *  worked examples. Rendered once per editor by default, or shared across a
 *  group of editors via MatchQueryMonaco's showExamples=false. */
export function MatchQueryExamples(): JSX.Element {
  return (
    <details className="group">
      <summary className="text-muted-foreground hover:text-foreground flex cursor-pointer list-none items-center gap-1 text-xs">
        <ChevronRight className="h-3 w-3 transition-transform group-open:rotate-90" />
        Query syntax & examples
      </summary>
      <div className="border-border bg-muted/30 mt-1.5 space-y-3 rounded-md border p-3">
        <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
          {MATCH_QUERY_SYNTAX.map((s) => (
            <Fragment key={s.token}>
              <HighlightedQuery query={s.token} />
              <span className="text-muted-foreground text-[11px]">
                {s.meaning}
              </span>
            </Fragment>
          ))}
        </div>
        <div className="border-border border-t pt-2 text-[11px] font-medium text-muted-foreground">
          Examples
        </div>
        <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
          {MATCH_QUERY_EXAMPLES.map((ex) => (
            <Fragment key={ex.query}>
              <HighlightedQuery query={ex.query} />
              <span className="text-muted-foreground text-[11px]">
                {ex.meaning}
              </span>
            </Fragment>
          ))}
        </div>
      </div>
    </details>
  );
}

/** Operator legend shown above the worked examples. */
const MATCH_QUERY_SYNTAX: { token: string; meaning: string }[] = [
  { token: "field:value", meaning: "matches exactly" },
  { token: "field:*text*", meaning: "contains (use * as a wildcard)" },
  { token: "field:/regex/", meaning: "matches a regular expression" },
  { token: "-field:value", meaning: "does NOT match (negate)" },
  { token: 'field:""', meaning: "is empty" },
  { token: "A AND B / A OR B", meaning: "combine clauses (don't mix the two)" },
];
