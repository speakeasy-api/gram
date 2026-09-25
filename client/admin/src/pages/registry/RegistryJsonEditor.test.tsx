import {
  cleanup,
  fireEvent,
  act,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Component } from "react";

const mock = vi.hoisted(() => ({
  value: '{"n":9007199254740993}',
  paste: null as null | (() => void),
  dispose: vi.fn(),
  undoStop: vi.fn(),
  edits: vi.fn(),
  markers: vi.fn(),
  options: {} as unknown,
  dom: null as HTMLElement | null,
}));
vi.mock("monaco-editor/internal/common/workers.js", () => ({}));
vi.mock("monaco-editor/editor/editor.api", () => ({
  editor: { EditorOption: { readOnly: 1 }, setModelMarkers: mock.markers },
  MarkerSeverity: { Error: 8 },
}));
vi.mock("monaco-editor/languages/features/json/register.js", () => ({
  jsonDefaults: {
    setDiagnosticsOptions: vi.fn(),
    setModeConfiguration: vi.fn(),
  },
}));
vi.mock("monaco-editor/editor/editor.worker?worker", () => ({
  default: class {},
}));
vi.mock("monaco-editor/language/json/json.worker?worker", () => ({
  default: class {},
}));
vi.mock("@monaco-editor/react", () => ({
  loader: { config: vi.fn() },
  default: class MockMonaco extends Component<{
    onMount: (editor: unknown) => void;
    options: unknown;
    theme?: string;
    path?: string;
  }> {
    componentDidMount() {
      mock.options = this.props.options;
      this.props.onMount({
        getModel: () => ({
          getValue: () => mock.value,
          getValueLength: () => mock.value.length,
          getFullModelRange: () => ({}),
          isDisposed: () => false,
          getPositionAt: (offset: number) => ({
            lineNumber: 1,
            column: offset + 1,
          }),
        }),
        getDomNode: () => mock.dom,
        getOption: () => false,
        pushUndoStop: mock.undoStop,
        executeEdits: mock.edits,
        onDidDispose: () => ({ dispose: vi.fn() }),
        onDidPaste: (callback: () => void) => {
          mock.paste = callback;
          return { dispose: mock.dispose };
        },
      });
    }
    render() {
      return (
        <div
          data-testid="monaco"
          data-theme={this.props.theme}
          data-path={this.props.path}
        />
      );
    }
  },
}));
import RegistryJsonEditor from "./RegistryJsonEditor";

const props = {
  value: "{}",
  onChange: vi.fn(),
  onBlur: vi.fn(),
  disabled: false,
  invalid: false,
  describedBy: "registry-feedback",
  issues: [],
};
afterEach(() => {
  cleanup();
  mock.dom = null;
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  mock.value = '{"n":9007199254740993}';
});

it("uses one undoable whitespace edit for format and paste; typing never formats", () => {
  render(<RegistryJsonEditor {...props} />);
  expect(mock.edits).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Format JSON" }));
  expect(mock.edits).toHaveBeenCalledTimes(1);
  expect(mock.undoStop).toHaveBeenCalledTimes(2);
  expect(mock.edits.mock.calls[0]![1][0].text).toContain("9007199254740993");
  mock.paste!();
  expect(mock.edits).toHaveBeenCalledTimes(2);
  mock.value = "{invalid";
  mock.paste!();
  expect(mock.edits).toHaveBeenCalledTimes(2);
});
it("clears markers and disposes paste subscription on unmount", () => {
  const { unmount } = render(<RegistryJsonEditor {...props} />);
  expect(mock.markers).toHaveBeenLastCalledWith(
    expect.anything(),
    "registry-server",
    [],
  );
  unmount();
  expect(mock.dispose).toHaveBeenCalledTimes(1);
});
it("keeps large drafts editable without mounting Monaco", () => {
  const value = " ".repeat(1024 * 1024 + 1);
  render(<RegistryJsonEditor {...props} value={value} />);
  expect(screen.queryByTestId("monaco")).toBeNull();
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toBe(value);
  expect(screen.queryByRole("button", { name: "Format JSON" })).toBeNull();
});

it("retains the mounted editor across oversized paste and undo-sized updates", () => {
  const { rerender } = render(<RegistryJsonEditor {...props} />);
  const editor = screen.getByTestId("monaco");
  const paste = mock.paste;
  mock.value = '"' + "x".repeat(1024 * 1024) + '"';
  mock.paste!();
  expect(mock.edits).not.toHaveBeenCalled();
  rerender(<RegistryJsonEditor {...props} value={mock.value} />);
  expect(screen.getByTestId("monaco")).toBe(editor);
  rerender(<RegistryJsonEditor {...props} value="{}" />);
  expect(screen.getByTestId("monaco")).toBe(editor);
  expect(mock.paste).toBe(paste);
  expect(mock.dispose).not.toHaveBeenCalled();
});

it("retains the editor when one undoable format edit crosses the threshold", () => {
  mock.value = '["' + "x".repeat(1024 * 1024 - 6) + '"]';
  const { rerender } = render(
    <RegistryJsonEditor {...props} value={mock.value} />,
  );
  const editor = screen.getByTestId("monaco");
  mock.paste!();
  expect(mock.edits).toHaveBeenCalledTimes(1);
  expect(mock.undoStop).toHaveBeenCalledTimes(2);
  const formatted = mock.edits.mock.calls[0]![1][0].text as string;
  expect(formatted.length).toBeGreaterThan(1024 * 1024);
  rerender(<RegistryJsonEditor {...props} value={formatted} />);
  expect(screen.getByTestId("monaco")).toBe(editor);
  expect(mock.dispose).not.toHaveBeenCalled();
});

it("keeps an initially large document in plain text after shrinking", () => {
  const { rerender } = render(
    <RegistryJsonEditor {...props} value={" ".repeat(1024 * 1024 + 1)} />,
  );
  const editor = screen.getByLabelText("Record JSON");
  rerender(<RegistryJsonEditor {...props} value="{}" />);
  expect(screen.getByLabelText("Record JSON")).toBe(editor);
  expect(screen.queryByTestId("monaco")).toBeNull();
});

it.each(["textarea", "div"])(
  "updates validation attributes on the actual %s textbox",
  (tag) => {
    mock.dom = document.createElement("div");
    const input = document.createElement(tag);
    if (tag === "div") input.setAttribute("role", "textbox");
    mock.dom.append(input);
    const { rerender } = render(
      <RegistryJsonEditor
        {...props}
        invalid
        describedBy="registry-feedback registry-request-error"
      />,
    );
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(input.getAttribute("aria-describedby")).toBe(
      "registry-feedback registry-request-error",
    );
    rerender(<RegistryJsonEditor {...props} />);
    expect(input.getAttribute("aria-invalid")).toBe("false");
    expect(input.getAttribute("aria-describedby")).toBe("registry-feedback");
  },
);

it.each([false, true])(
  "follows live system theme without replacing the model (dark=%s)",
  (dark) => {
    const media = new EventTarget();
    Object.assign(media, { matches: dark });
    vi.stubGlobal("matchMedia", vi.fn().mockReturnValue(media));
    render(<RegistryJsonEditor {...props} />);
    const editor = screen.getByTestId("monaco");
    const path = editor.getAttribute("data-path");
    expect(editor.getAttribute("data-theme")).toBe(dark ? "vs-dark" : "vs");
    act(() => {
      Object.assign(media, { matches: !dark });
      media.dispatchEvent(new Event("change"));
    });
    expect(screen.getByTestId("monaco")).toBe(editor);
    expect(editor.getAttribute("data-path")).toBe(path);
    expect(editor.getAttribute("data-theme")).toBe(dark ? "vs" : "vs-dark");
    expect(mock.dispose).not.toHaveBeenCalled();
    expect(mock.edits).not.toHaveBeenCalled();
  },
);
