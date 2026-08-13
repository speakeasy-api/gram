"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type FC,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { useAui, useAuiState } from "@assistant-ui/react";

import {
  caretOffset,
  readPlainText,
  selectionOffsets,
  setCaret,
} from "@/elements/lib/composer-dom";
import { REFERENCE_TOKEN_CLASSES } from "@/elements/lib/reference-token-classes";
import {
  splitComposerSegments,
  type ToolRecord,
} from "@/elements/lib/tool-mentions";
import { cn } from "@/lib/utils";

export interface ComposerRichInputProps {
  placeholder?: string;
  className?: string;
  autoFocus?: boolean;
  disabled?: boolean;
  tools: ToolRecord;
  skillNames: readonly string[];
  onKeyDown?: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  /** Enter without Shift: the form owns sending, same as the textarea did. */
  onSubmit: () => void;
}

/**
 * The composer input, as a contenteditable rather than a textarea.
 *
 * A textarea holds one flat string, so an `@tool` / `/skill` reference inside a
 * draft could only ever be *painted* — by mirroring the text under a
 * transparent input — and a painted chip may not occupy any width, or the
 * caret (positioned from the textarea's own metrics) drifts off the glyphs. A
 * contenteditable makes the reference a real inline element: it can carry real
 * padding, and the browser places the caret around it.
 *
 * The draft still lives on the runtime as a string. This renders that string,
 * reports edits back as a string, and restores the caret by character offset
 * after each controlled re-render — so nothing downstream learns about the tree.
 */
export const ComposerRichInput: FC<ComposerRichInputProps> = ({
  placeholder,
  className,
  autoFocus,
  disabled,
  tools,
  skillNames,
  onKeyDown,
  onSubmit,
}) => {
  const aui = useAui();
  const text = useAuiState(({ composer }) => composer.text);
  const ref = useRef<HTMLDivElement>(null);
  /** Where to put the caret once React has rewritten the tree. */
  const pendingCaret = useRef<number | null>(null);
  /** True while an IME is composing: the DOM is mid-edit and must be left alone. */
  const composing = useRef(false);

  const segments = splitComposerSegments(text, tools, skillNames);
  // What the tree should look like, as a string. Rebuilding only when THIS
  // changes keeps ordinary typing off the imperative path, so the browser's own
  // undo stack and IME survive.
  const signature = segments
    .map((segment) => `${segment.kind}:${segment.text}`)
    .join("\u0000");
  const renderedSignature = useRef<string | null>(null);
  /** The last draft this input itself reported. Anything else arriving as
   *  `text` came from outside (dictation, prompt recall, the context picker)
   *  and is authoritative over what is currently in the DOM. */
  const lastReported = useRef<string | null>(null);

  const handleInput = useCallback(() => {
    const element = ref.current;
    if (!element || composing.current) return;
    const next = readPlainText(element);
    pendingCaret.current = caretOffset(element);
    lastReported.current = next;
    aui.composer().setText(next);
  }, [aui]);

  // React must NOT own these children. The browser edits this subtree directly
  // on every keystroke, so React's picture of the DOM goes stale immediately
  // and its next reconcile writes new text into nodes the user already changed
  // — which duplicates the draft. The tree is rebuilt here instead, and only
  // when the token layout actually changed.
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || composing.current) return;

    const stale =
      renderedSignature.current !== signature ||
      readPlainText(element) !== text;
    if (!stale) {
      pendingCaret.current = null;
      return;
    }

    element.replaceChildren(
      ...segments.map((segment) => {
        if (segment.kind === "text") {
          return document.createTextNode(segment.text);
        }
        const chip = document.createElement("span");
        // Atomic to the caret: arrowing or backspacing treats the reference as
        // one thing, the way a mention behaves everywhere else.
        chip.contentEditable = "false";
        chip.dataset.reference = segment.kind;
        chip.className = REFERENCE_TOKEN_CLASSES.surface[segment.kind];
        chip.textContent = segment.text;
        return chip;
      }),
    );
    renderedSignature.current = signature;
    lastReported.current = text;

    const target = pendingCaret.current ?? text.length;
    pendingCaret.current = null;
    const root = element.getRootNode();
    const active =
      root instanceof ShadowRoot ? root.activeElement : document.activeElement;
    if (active !== element) return;
    setCaret(element, Math.min(target, text.length));
  }, [segments, signature, text]);

  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  // Everything else that touches the input — the @-mention autocomplete, prompt
  // recall, the context picker's focus handoff — was written against a
  // textarea and finds this element by `.aui-composer-input`. Rather than teach
  // each of them about ranges, the element answers the three textarea
  // properties they use, computed from the tree.
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    Object.defineProperties(element, {
      value: {
        configurable: true,
        get: () => readPlainText(element),
      },
      selectionStart: {
        configurable: true,
        get: () => selectionOffsets(element)?.start ?? 0,
      },
      selectionEnd: {
        configurable: true,
        get: () => selectionOffsets(element)?.end ?? 0,
      },
      setSelectionRange: {
        configurable: true,
        value: (start: number, end?: number) =>
          setCaret(element, start, end ?? start),
      },
    });
  }, []);

  return (
    <div
      ref={ref}
      // `plaintext-only` keeps the browser from injecting its own markup on
      // paste or Enter, which is the whole reason a rich composer usually needs
      // a sanitizer. The tree stays exactly what this component renders.
      contentEditable={disabled ? false : "plaintext-only"}
      suppressContentEditableWarning
      role="textbox"
      aria-multiline="true"
      aria-label="Message input"
      data-placeholder={placeholder}
      data-empty={text === "" ? "true" : undefined}
      className={cn("aui-composer-input", className)}
      onInput={handleInput}
      onCompositionStart={() => {
        composing.current = true;
      }}
      onCompositionEnd={() => {
        composing.current = false;
        handleInput();
      }}
      onKeyDown={(event) => {
        onKeyDown?.(event);
        if (event.defaultPrevented) return;
        if (event.key === "Enter" && !event.shiftKey && !composing.current) {
          event.preventDefault();
          onSubmit();
        }
      }}
    />
  );
};
