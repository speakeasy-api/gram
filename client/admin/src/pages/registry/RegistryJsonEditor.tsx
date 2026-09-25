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

export default class RegistryJsonEditor extends Component<RegistryJsonEditorProps> {
  // Admin CSS follows the system preference; it has no theme override.
  private readonly colorScheme = window.matchMedia(
    "(prefers-color-scheme: dark)",
  );
  state = { mounted: false, dark: this.colorScheme.matches };

  componentDidMount(): void {
    this.colorScheme.addEventListener("change", this.updateTheme);
  }

  private updateTheme = (): void => {
    this.setState({ dark: this.colorScheme.matches });
  };

  // Fix presentation at mount: replacing the editor during an edit loses undo.
  private readonly large = this.props.value.length > RICH_EDITOR_LIMIT;
  private readonly modelPath = `gram-registry://draft/${crypto.randomUUID()}.json`;
  private editor: monaco.editor.IStandaloneCodeEditor | null = null;
  private subscriptions: monaco.IDisposable[] = [];

  componentWillUnmount(): void {
    this.colorScheme.removeEventListener("change", this.updateTheme);
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

  private onMount: OnMount = (current) => {
    this.editor = current;
    this.subscriptions = [
      current.onDidPaste(this.formatDocument),
      current.onDidDispose(this.disposeSubscriptions),
    ];
    this.setState({ mounted: true });
  };

  componentDidUpdate(): void {
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
    if (!model || model.isDisposed() || value.length > RICH_EDITOR_LIMIT)
      return;
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
      <div>
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
              className="border-input min-h-96 w-full border p-3 font-mono text-sm"
              value={value}
              disabled={disabled}
              onChange={(event) => onChange(event.target.value)}
              onBlur={onBlur}
            />
          </>
        ) : (
          <>
            <Button
              variant="outline"
              disabled={disabled || !this.state.mounted}
              onClick={this.formatDocument}
            >
              Format JSON
            </Button>
            <div onBlur={onBlur}>
              <MonacoEditor
                height="384px"
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
                  accessibilitySupport: "on",
                  maxTokenizationLineLength: 10000,
                }}
              />
            </div>
            <a href="#registry-feedback" className="text-sm underline">
              Validation feedback
            </a>
          </>
        )}
      </div>
    );
  }
}
