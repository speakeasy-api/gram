import { Component, type JSX } from "react";
import MonacoEditor, { loader, type OnMount } from "@monaco-editor/react";
import * as monaco from "monaco-editor/editor/editor.api";
// Monaco 0.56 JSON workers load this bootstrap too. Register its standalone
// services before the first editor initializes, not after the worker starts.
import "monaco-editor/internal/common/workers.js";
import { registryJsonDiagnostics } from "./RegistryJsonSchema";
import { jsonDefaults } from "monaco-editor/languages/features/json/register.js";
// oxlint-disable-next-line import/default -- Vite ?worker supplies the constructor export
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
// oxlint-disable-next-line import/default -- Vite ?worker supplies the constructor export
import JsonWorker from "monaco-editor/language/json/json.worker?worker";
import { Button } from "@/components/ui/button";
import {
  formatRegistryJson,
  registryIssueOffsets,
} from "@/lib/registryJsonEditor";
import type { ValidationIssue } from "@/lib/registryValidation";

self.MonacoEnvironment = {
  getWorker: (_moduleId, label) =>
    label === "json" ? new JsonWorker() : new EditorWorker(),
};
loader.config({ monaco });
jsonDefaults.setDiagnosticsOptions(registryJsonDiagnostics);

jsonDefaults.setModeConfiguration({
  documentFormattingEdits: false,
  documentRangeFormattingEdits: false,
  completionItems: true,
  hovers: false,
  documentSymbols: false,
  tokens: true,
  colors: false,
  foldingRanges: false,
  diagnostics: true,
  selectionRanges: false,
});

export type RegistryJsonEditorProps = {
  value: string;
  onChange: (value: string) => void;
  onBlur: () => void;
  disabled: boolean;
  invalid: boolean;
  describedBy: string;
  issues: ValidationIssue[];
};

// Keep the existing 8 MiB contract without running Monaco/AST work over huge
// documents. This is a presentation fallback, not a smaller input limit.
const RICH_EDITOR_LIMIT = 1024 * 1024;
// The editor grows with its content so the page, not the editor, scrolls. This
// keeps an empty or short record from collapsing to a sliver.
const MIN_EDITOR_HEIGHT = 128;

export default class RegistryJsonEditor extends Component<RegistryJsonEditorProps> {
  // Admin CSS follows the system preference; it has no theme override.
  private readonly colorScheme = window.matchMedia(
    "(prefers-color-scheme: dark)",
  );
  state = {
    mounted: false,
    dark: this.colorScheme.matches,
    height: MIN_EDITOR_HEIGHT,
  };

  componentDidMount(): void {
    this.colorScheme.addEventListener("change", this.updateTheme);
    this.fitTextarea();
    // Wrapping depends on width. Refit only when the width changes, since a
    // refit changes the height and would otherwise notify again.
    if (this.textarea && typeof ResizeObserver !== "undefined") {
      let width: number | undefined;
      this.resizeObserver = new ResizeObserver(([entry]) => {
        const next = entry?.contentRect.width;
        if (next === undefined || next === width) return;
        const first = width === undefined;
        width = next;
        if (!first) this.fitTextarea();
      });
      this.resizeObserver.observe(this.textarea);
    }
  }

  private updateTheme = (): void => {
    this.setState({ dark: this.colorScheme.matches });
  };

  // Fix presentation at mount: replacing the editor during an edit loses undo.
  private readonly large = this.props.value.length > RICH_EDITOR_LIMIT;
  private readonly modelPath = `gram-registry://draft/${crypto.randomUUID()}.json`;
  private editor: monaco.editor.IStandaloneCodeEditor | null = null;
  private textarea: HTMLTextAreaElement | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private subscriptions: monaco.IDisposable[] = [];

  componentWillUnmount(): void {
    this.colorScheme.removeEventListener("change", this.updateTheme);
    this.resizeObserver?.disconnect();
    this.disposeSubscriptions();
  }

  private disposeSubscriptions = (): void => {
    for (const subscription of this.subscriptions) subscription.dispose();
    this.subscriptions = [];
    this.editor = null;
  };

  private formatDocument = (): void => {
    const current = this.editor;
    const model = current?.getModel();
    if (
      !current ||
      !model ||
      current.getOption(monaco.editor.EditorOption.readOnly)
    )
      return;
    if (model.getValueLength() > RICH_EDITOR_LIMIT) return;
    const formatted = formatRegistryJson(model.getValue());
    if (formatted === null || formatted === model.getValue()) return;
    current.pushUndoStop();
    current.executeEdits("registry-format", [
      { range: model.getFullModelRange(), text: formatted },
    ]);
    current.pushUndoStop();
  };

  private fitEditor = (): void => {
    const height = Math.max(
      MIN_EDITOR_HEIGHT,
      this.editor?.getContentHeight() ?? 0,
    );
    if (height !== this.state.height) this.setState({ height });
  };

  // Measure from `auto` so the box can shrink as well as grow; the border is
  // added back because scrollHeight leaves it out. The holder keeps its height
  // while measuring so the page does not collapse and lose its scroll position.
  private fitTextarea = (): void => {
    const textarea = this.textarea;
    if (!textarea) return;
    const holder = textarea.parentElement;
    if (holder) holder.style.minHeight = `${holder.offsetHeight}px`;
    textarea.style.height = "auto";
    const border = textarea.offsetHeight - textarea.clientHeight;
    textarea.style.height = `${Math.max(MIN_EDITOR_HEIGHT, textarea.scrollHeight + border)}px`;
    if (holder) holder.style.minHeight = "";
  };

  private onMount: OnMount = (current) => {
    this.editor = current;
    this.subscriptions = [
      current.onDidPaste(this.formatDocument),
      current.onDidContentSizeChange(this.fitEditor),
      current.onDidDispose(this.disposeSubscriptions),
    ];
    this.setState({ mounted: true });
    this.fitEditor();
  };

  componentDidUpdate(previous: RegistryJsonEditorProps): void {
    if (previous.value !== this.props.value) this.fitTextarea();
    const { value, describedBy, invalid, issues } = this.props;
    const editor = this.editor;
    const inputs = editor
      ?.getDomNode()
      ?.querySelectorAll('textarea, [role="textbox"]');
    inputs?.forEach((input) => {
      input.setAttribute("aria-describedby", describedBy);
      input.setAttribute("aria-invalid", String(invalid));
    });
    const model = editor?.getModel();
    if (!model || model.isDisposed()) return;
    const oversized = value.length > RICH_EDITOR_LIMIT;
    const language = oversized ? "plaintext" : "json";
    // Changing only this model's language unregisters JSON diagnostics (including
    // pending results), while preserving its undo stack, cursor and selection.
    if (model.getLanguageId() !== language)
      monaco.editor.setModelLanguage(model, language);
    if (oversized) {
      monaco.editor.setModelMarkers(model, "registry-server", []);
      monaco.editor.setModelMarkers(model, "json", []);
      return;
    }
    monaco.editor.setModelMarkers(
      model,
      "registry-server",
      (issues.length ? registryIssueOffsets(value, issues) : []).map(
        (issue) => {
          const start = model.getPositionAt(issue.offset);
          const end = model.getPositionAt(issue.offset + issue.length);
          return {
            severity: monaco.MarkerSeverity.Error,
            message: issue.message,
            startLineNumber: start.lineNumber,
            startColumn: start.column,
            endLineNumber: end.lineNumber,
            endColumn: end.column,
          };
        },
      ),
    );
  }

  render(): JSX.Element {
    const { value, onChange, onBlur, disabled, invalid, describedBy } =
      this.props;
    return (
      <div className="flex flex-col gap-2">
        <div className="flex shrink-0 items-center justify-between gap-3">
          <p className="text-sm font-medium">Record JSON</p>
          {!this.large && (
            <Button
              variant="outline"
              disabled={
                disabled ||
                !this.state.mounted ||
                value.length > RICH_EDITOR_LIMIT
              }
              onClick={this.formatDocument}
            >
              Format JSON
            </Button>
          )}
        </div>
        {this.large ? (
          <>
            <p className="text-muted-foreground text-sm">
              Large record: plain-text editing. Syntax and size are checked on
              blur and Save; inline diagnostics and formatting are paused.
            </p>
            <textarea
              aria-label="Record JSON"
              aria-describedby={describedBy}
              aria-invalid={invalid}
              ref={(element) => {
                this.textarea = element;
              }}
              className="border-input w-full resize-none overflow-hidden rounded-md border p-3 font-mono text-sm"
              value={value}
              disabled={disabled}
              onChange={(event) => onChange(event.target.value)}
              onBlur={onBlur}
            />
          </>
        ) : (
          <>
            <div
              // Content box, so the measured height is the editor's own.
              className="border-input box-content overflow-hidden rounded-md border py-2"
              style={{ height: this.state.height }}
              onBlur={onBlur}
            >
              <MonacoEditor
                height="100%"
                language="json"
                theme={this.state.dark ? "vs-dark" : "vs"}
                path={this.modelPath}
                value={value}
                onChange={(next) => onChange(next ?? "")}
                onMount={this.onMount}
                options={{
                  ariaLabel: "Record JSON",
                  ariaRequired: false,
                  readOnly: disabled,
                  tabSize: 2,
                  insertSpaces: true,
                  minimap: { enabled: false },
                  lineNumbers: "on",
                  quickSuggestions: {
                    other: true,
                    strings: true,
                    comments: false,
                  },
                  suggestOnTriggerCharacters: true,
                  acceptSuggestionOnEnter: "off",
                  tabCompletion: "off",
                  wordBasedSuggestions: "off",
                  parameterHints: { enabled: false },
                  formatOnPaste: false,
                  formatOnType: false,
                  automaticLayout: true,
                  scrollBeyondLastLine: false,
                  // Nothing to scroll inside, so the wheel scrolls the page.
                  scrollbar: { alwaysConsumeMouseWheel: false },
                  accessibilitySupport: "on",
                  maxTokenizationLineLength: 10000,
                }}
              />
            </div>
          </>
        )}
      </div>
    );
  }
}
