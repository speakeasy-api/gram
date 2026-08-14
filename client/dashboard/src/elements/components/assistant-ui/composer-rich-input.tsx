"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
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
  type ComposerSegment,
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
/** Cap on un-echoed local drafts; keystrokes in flight at once. */
const ECHO_QUEUE_LIMIT = 16;

/** How long after the last keystroke a token may redraw. Long enough to clear
 *  a burst of queued input, short enough to read as immediate. */
const TYPING_SETTLE_MS = 120;

/**
 * The token layout only — which references exist, and in what order. Plain runs
 * are deliberately left out: including them would make every keystroke a
 * rebuild, and a rebuilt subtree carries no undo history, so Cmd+Z would stop
 * working in the composer.
 */
function tokenSignature(segments: ComposerSegment[]): string {
  return segments
    .filter((segment) => segment.kind !== "text")
    .map((segment) => `${segment.kind}:${segment.text}`)
    .join("\u0000");
}

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
  const signature = tokenSignature(segments);
  const renderedSignature = useRef<string | null>(null);
  /** Drafts this input reported and has not yet seen come back, oldest first.
   *
   *  A queue, not a single value: several keystrokes can land before the render
   *  for the first of them arrives, and that render carries a string this input
   *  DID report, just not most recently — mistaking it for an outside edit
   *  rebuilds the tree from it and eats the newer keystrokes. Each render
   *  CONSUMES its entry, so a later external draft that happens to equal
   *  something typed earlier is still recognised as external. */
  const awaitingEcho = useRef<string[]>([]);

  /** Set while keystrokes are still arriving; a rebuild waits for the lull. */
  const typingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** An outside draft arrived mid-burst and has not been painted yet. */
  const externalPending = useRef(false);
  const [idleTick, setIdleTick] = useState(0);

  const handleInput = useCallback(() => {
    const element = ref.current;
    if (!element || composing.current) return;
    // An outside draft landed mid-burst and is still waiting to be painted.
    // Reporting the DOM now would report the text it is about to replace, and
    // the recalled prompt (or inserted token) would be gone. Let it through
    // first; this keystroke is the cheaper thing to drop.
    if (externalPending.current) {
      if (typingTimer.current) clearTimeout(typingTimer.current);
      typingTimer.current = null;
      setIdleTick((tick) => tick + 1);
      return;
    }
    const next = readPlainText(element);
    pendingCaret.current = caretOffset(element);
    awaitingEcho.current = [
      ...awaitingEcho.current.slice(-(ECHO_QUEUE_LIMIT - 1)),
      next,
    ];
    // Rebuilding while keystrokes are still queued destroys the very nodes the
    // browser is about to apply them to, and those characters are simply lost.
    // Every keystroke pushes the rebuild out; it runs once typing settles.
    if (typingTimer.current) clearTimeout(typingTimer.current);
    typingTimer.current = setTimeout(() => {
      typingTimer.current = null;
      setIdleTick((tick) => tick + 1);
    }, TYPING_SETTLE_MS);
    aui.composer().setText(next);
  }, [aui]);

  useEffect(
    () => () => {
      if (typingTimer.current) clearTimeout(typingTimer.current);
    },
    [],
  );

  // React must NOT own these children. The browser edits this subtree directly
  // on every keystroke, so React's picture of the DOM goes stale immediately
  // and its next reconcile writes new text into nodes the user already changed
  // — which duplicates the draft. The tree is rebuilt here instead, and only
  // when the token layout actually changed.
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || composing.current) return;
    // Whose string is newer. A draft this input reported can arrive back
    // several keystrokes late — the browser has already applied them to the
    // tree — so for local edits the DOM is the truth and rebuilding from
    // `text` would swallow them. Only a change from elsewhere (dictation,
    // prompt recall, an inserted token) outranks what is on screen.
    const domText = readPlainText(element);
    // Where the caret sits RIGHT NOW, not where it sat when the render that
    // triggered this was queued: a deferred rebuild can run several keystrokes
    // later, and restoring the older offset drops the caret mid-word so the
    // next characters land in the wrong place.
    const liveCaret = caretOffset(element);
    const echoed = awaitingEcho.current.indexOf(text);
    if (echoed !== -1)
      awaitingEcho.current = awaitingEcho.current.slice(echoed + 1);
    const fromOutside = echoed === -1;
    const source = fromOutside ? text : domText;
    const sourceSegments = fromOutside
      ? segments
      : splitComposerSegments(source, tools, skillNames);
    const sourceSignature = tokenSignature(sourceSegments);

    if (sourceSignature === renderedSignature.current && domText === source) {
      pendingCaret.current = null;
      return;
    }

    // Nothing redraws mid-burst, whoever asked: replacing these nodes while
    // keystrokes are still queued against them loses those characters. The
    // timer's own tick re-runs this the moment typing settles — but an outside
    // draft waiting behind it must not be quietly overwritten by the next
    // keystroke reporting the stale DOM, so flag it for `handleInput`.
    if (typingTimer.current) {
      if (fromOutside) externalPending.current = true;
      return;
    }
    externalPending.current = false;
    element.replaceChildren(
      ...sourceSegments.map((segment) => {
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
    renderedSignature.current = sourceSignature;
    // Entries newer than what was just painted stay queued: they are keystrokes
    // this input reported and has yet to see come back, and dropping them makes
    // their renders look like outside edits — which repaints mid-burst and
    // scrambles the draft.
    const painted = awaitingEcho.current.indexOf(source);
    if (painted !== -1) {
      awaitingEcho.current = awaitingEcho.current.slice(painted + 1);
    }
    // The runtime still owns the draft, so a rebuild from the DOM has to report
    // what it rendered — otherwise state keeps the older string forever.
    if (source !== text) {
      awaitingEcho.current = [...awaitingEcho.current, source];
      aui.composer().setText(source);
    }

    // An outside draft brings its own caret (the inserter asked for one, or it
    // belongs at the end of the new text); a local redraw keeps the user's.
    const target = fromOutside
      ? (pendingCaret.current ?? source.length)
      : (liveCaret ?? pendingCaret.current ?? source.length);
    pendingCaret.current = null;
    const root = element.getRootNode();
    const active =
      root instanceof ShadowRoot ? root.activeElement : document.activeElement;
    if (active !== element) return;
    setCaret(element, Math.min(target, source.length));
  }, [aui, idleTick, segments, signature, skillNames, text, tools]);

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
