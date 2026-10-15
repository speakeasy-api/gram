import { CircleHelpIcon, SearchIcon, XIcon } from "lucide-react";
import { useId, useLayoutEffect, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  parseUserSearch,
  serializeUserSearch,
  type UserSearchTerm,
} from "@/lib/userSearch";

export type UserSearchInputProps = {
  value: string;
  onChange: (value: string) => void;
  error?: string;
  onClear?: () => void;
};

type Piece = { text: string; term?: Pick<UserSearchTerm, "field" | "value"> };
type Editor = { pieces: Piece[]; editing?: { index: number; original: Piece } };
type Change = { before: Editor; after: Editor };
const queryOf = (editor: Editor) =>
  editor.pieces
    .map((piece) => piece.text)
    .filter(Boolean)
    .join(" ");

function restoreEdit(editor: Editor): Editor {
  const editing = editor.editing;
  if (!editing) return editor;
  return {
    pieces: editor.pieces.map((piece, index) =>
      index === editing.index ? editing.original : piece,
    ),
  };
}

// Source spans preserve order, including editable bare text between filters.
// The parser alone decides whether a term is complete and supported.
function tokenize(query: string): Editor {
  const parsed = parseUserSearch(query);
  if (!parsed.ok) return { pieces: [{ text: query }] };
  const pieces: Piece[] = [];
  let bareStart: number | undefined;
  let bareEnd = 0;
  for (const term of parsed.terms) {
    if (term.field === "any") {
      bareStart ??= term.start;
      bareEnd = term.end;
      continue;
    }
    if (bareStart !== undefined) {
      pieces.push({ text: query.slice(bareStart, bareEnd) });
      bareStart = undefined;
    }
    pieces.push({
      text: query.slice(term.start, term.end),
      term: { field: term.field, value: term.value },
    });
  }
  pieces.push({
    text: bareStart === undefined ? "" : query.slice(bareStart, bareEnd),
  });
  return { pieces };
}

export function UserSearchInput({
  value,
  onChange,
  error,
  onClear,
}: UserSearchInputProps): React.JSX.Element {
  const hintId = useId();
  const [editor, setEditor] = useState(() => tokenize(value));
  const [seenValue, setSeenValue] = useState(value);
  const lastEmitted = useRef<string | undefined>(undefined);
  const undo = useRef<Change[]>([]);
  const redo = useRef<Change[]>([]);
  const composing = useRef(false);
  const inputs = useRef(new Map<number, HTMLInputElement>());
  const focus = useRef<{ index: number; caret?: number } | null>(null);

  // A synchronous parent's echo must not turn the current edit back into a pill.
  // Genuine URL restores repaint immediately and cannot undo into another URL.
  if (value !== seenValue) {
    setSeenValue(value);
    if (value !== lastEmitted.current) {
      setEditor(tokenize(value));
      undo.current = [];
      redo.current = [];
      focus.current = null;
    }
    lastEmitted.current = undefined;
  }

  useLayoutEffect(() => {
    if (focus.current === null) return;
    const { index, caret } = focus.current;
    const input = inputs.current.get(index);
    input?.focus();
    if (caret !== undefined) input?.setSelectionRange(caret, caret);
    focus.current = null;
  }, [editor]);

  function update(
    next: Editor,
    transformation = false,
    focusIndex?: number,
    settleEdit = false,
    caret?: number,
  ) {
    let before = editor;
    if (settleEdit && editor.editing) {
      const original = editor.editing.original;
      const settle = (state: Editor) =>
        state.editing?.original === original ? restoreEdit(state) : state;
      // Settling this edit turns its opener into a no-op; any other snapshots
      // keep their own before/after pieces with the original filter restored.
      undo.current = undo.current
        .map((change) => ({
          before: settle(change.before),
          after: settle(change.after),
        }))
        .filter(
          (change) =>
            JSON.stringify(change.before) !== JSON.stringify(change.after),
        );
      before = restoreEdit(editor);
    }
    if (transformation && JSON.stringify(before) !== JSON.stringify(next)) {
      undo.current.push({ before, after: next });
    }
    redo.current = [];
    setEditor(next);
    if (focusIndex !== undefined) focus.current = { index: focusIndex, caret };
    const query = queryOf(next);
    if (query !== queryOf(editor)) {
      lastEmitted.current = query;
      onChange(query);
    }
  }

  // Backspace at the start of text turns the pill just before it back into
  // its source text minus the final grapheme, merged with the neighbouring
  // text so further Backspaces keep deleting natively.
  function backspaceIntoPill(index: number): boolean {
    const pill = editor.pieces[index - 1];
    if (!pill?.term) return false;
    const before = editor.pieces[index - 2];
    const start = before && !before.term ? index - 2 : index - 1;
    // An editing piece is followed by its own draft; merge that as well.
    const after = editor.pieces[index + 1];
    const end = after && !after.term ? index + 1 : index;
    // An edit outside the merged text settles by restoring it, as elsewhere.
    const editing = editor.editing?.index;
    const base =
      editing !== undefined && (editing < start || editing > end)
        ? restoreEdit(editor)
        : editor;
    const last = Array.from(new Intl.Segmenter().segment(pill.text)).at(-1);
    const head = [
      start < index - 1 ? base.pieces[start]!.text : "",
      pill.text.slice(0, last?.index),
    ]
      .filter(Boolean)
      .join(" ");
    const text = [head, ...base.pieces.slice(index, end + 1).map((p) => p.text)]
      .filter(Boolean)
      .join(" ");
    const pieces = [
      ...base.pieces.slice(0, start),
      { text },
      ...base.pieces.slice(end + 1),
    ];
    update({ pieces }, true, start, editing !== undefined, head.length);
    return true;
  }

  function commit(next: Editor, index: number): boolean {
    const active = parseUserSearch(next.pieces[index]!.text);
    if (
      !active.ok ||
      (!active.terms.some((term) => term.field !== "any") &&
        next.editing?.index !== index)
    )
      return false;
    // Starting a transformation elsewhere settles the prior edit by restoring
    // it, just as activating another pill does. Existing pills alone never
    // make a plain text delimiter into a token operation.
    const source =
      next.editing && next.editing.index !== index ? restoreEdit(next) : next;
    const parsed = parseUserSearch(queryOf(source));
    if (!parsed.ok) return false;
    const tokenized = tokenize(queryOf(source));
    if (JSON.stringify(tokenized) === JSON.stringify(next)) return false;
    update(tokenized, true, tokenized.pieces.length - 1, true);
    return true;
  }

  function history(event: React.KeyboardEvent) {
    if (
      composing.current ||
      event.nativeEvent.isComposing ||
      !(event.ctrlKey || event.metaKey) ||
      event.altKey
    )
      return;
    const isRedo =
      (event.key.toLowerCase() === "z" && event.shiftKey) ||
      event.key.toLowerCase() === "y";
    if (!isRedo && event.key.toLowerCase() !== "z") return;
    const source = isRedo ? redo : undo;
    const change = source.current.at(-1);
    // Text changes belong to the browser. Only intercept at a transformation
    // boundary; native undo may bring the text back to this boundary later.
    if (
      !change ||
      JSON.stringify(editor) !==
        JSON.stringify(isRedo ? change.before : change.after)
    )
      return;
    event.preventDefault();
    source.current.pop();
    (isRedo ? undo : redo).current.push(change);
    const next = isRedo ? change.after : change.before;
    setEditor(next);
    focus.current = { index: next.editing?.index ?? next.pieces.length - 1 };
    lastEmitted.current = queryOf(next);
    onChange(queryOf(next));
  }

  const parsed = parseUserSearch(queryOf(editor));
  const help =
    "Use name:, email:, or org:. Space or Enter creates a filter; Escape cancels editing.";
  const hint = error || (!parsed.ok ? parsed.message : help);

  return (
    <div className="min-w-0 space-y-1">
      <div
        className="flex min-w-0 flex-wrap items-center gap-2"
        onKeyDown={history}
      >
        <div
          role="group"
          aria-label="User search"
          data-invalid={Boolean(error || !parsed.ok) || undefined}
          data-clearable={Boolean(onClear) || undefined}
          className="border-input focus-within:border-ring focus-within:ring-ring/50 data-[invalid]:border-destructive data-[invalid]:ring-destructive/20 dark:data-[invalid]:ring-destructive/40 dark:bg-input/30 relative flex min-h-9 min-w-0 flex-1 flex-wrap items-center gap-1 rounded-md border py-1 pr-3 pl-3 data-[clearable]:pr-9 shadow-xs transition-[color,box-shadow] focus-within:ring-[3px]"
          onClick={(event) => {
            if (event.target === event.currentTarget)
              inputs.current.get(editor.pieces.length - 1)?.focus();
          }}
        >
          <SearchIcon
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none mr-1 size-4 shrink-0"
          />
          {editor.pieces.map((piece, index) =>
            piece.term ? (
              <Badge
                key={`filter-${index}`}
                variant="secondary"
                // py-px makes the pill as tall as the h-7 text input, so
                // turning text into a pill does not grow the field.
                className="max-w-full gap-0 py-px"
              >
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  className="min-w-0 whitespace-normal break-all"
                  aria-label={`Edit ${piece.term.field} filter: ${piece.term.value}`}
                  aria-describedby={hintId}
                  onClick={() => {
                    const pieces = restoreEdit(editor).pieces.map((p, i) =>
                      i === index
                        ? { text: serializeUserSearch([piece.term!]) }
                        : p,
                    );
                    update(
                      { pieces, editing: { index, original: piece } },
                      true,
                      index,
                      true,
                    );
                  }}
                >
                  {piece.term.field}: {piece.term.value}
                </Button>
              </Badge>
            ) : (
              <Input
                key={
                  index === editor.pieces.length - 1 ? "draft" : `text-${index}`
                }
                ref={(node) => {
                  if (node) inputs.current.set(index, node);
                  else inputs.current.delete(index);
                }}
                aria-label={
                  index === editor.pieces.length - 1
                    ? "Search users"
                    : `Search text before filter ${index + 1}`
                }
                aria-describedby={hintId}
                aria-invalid={Boolean(error || !parsed.ok)}
                // Only the trailing draft fills the rest of the line. Bare text
                // and an editing pill hug their text so the field reads as one
                // run of content rather than evenly split columns.
                data-draft={index === editor.pieces.length - 1 || undefined}
                size={Math.max(piece.text.length, 1)}
                className="field-sizing-content h-7 w-auto min-w-[2ch] flex-none rounded-none border-0 px-1 shadow-none focus-visible:ring-0 data-[draft]:min-w-24 data-[draft]:flex-1 dark:bg-transparent"
                placeholder={
                  editor.pieces.length === 1 ? "Search users…" : undefined
                }
                value={piece.text}
                onCompositionStart={() => {
                  composing.current = true;
                }}
                onCompositionEnd={() => {
                  composing.current = false;
                }}
                onChange={(event) =>
                  update({
                    ...editor,
                    pieces: editor.pieces.map((p, i) =>
                      i === index ? { text: event.target.value } : p,
                    ),
                  })
                }
                onPaste={(event) => {
                  if (composing.current) return;
                  const text = event.clipboardData.getData("text/plain");
                  const target = event.currentTarget;
                  const pasted =
                    piece.text.slice(0, target.selectionStart ?? 0) +
                    text +
                    piece.text.slice(target.selectionEnd ?? piece.text.length);
                  const next = {
                    ...editor,
                    pieces: editor.pieces.map((p, i) =>
                      i === index ? { text: pasted } : p,
                    ),
                  };
                  // Leave ordinary/invalid paste to the browser's native text history.
                  const result = parseUserSearch(pasted);
                  if (
                    result.ok &&
                    result.terms.some((term) => term.field !== "any") &&
                    commit(next, index)
                  )
                    event.preventDefault();
                }}
                onKeyDown={(event) => {
                  if (
                    composing.current ||
                    event.nativeEvent.isComposing ||
                    event.keyCode === 229
                  )
                    return;
                  if (event.key === "Escape" && editor.editing) {
                    event.preventDefault();
                    const { index: editingIndex, original } = editor.editing;
                    update(
                      {
                        pieces: editor.pieces.map((p, i) =>
                          i === editingIndex ? original : p,
                        ),
                      },
                      true,
                      editor.pieces.length - 1,
                      true,
                    );
                  } else if (
                    (event.key === " " || event.key === "Enter") &&
                    !event.ctrlKey &&
                    !event.metaKey &&
                    !event.altKey
                  ) {
                    // Never split text at the caret, or swallow a space within quotes.
                    if (
                      event.currentTarget.selectionStart ===
                        piece.text.length &&
                      event.currentTarget.selectionEnd === piece.text.length &&
                      commit(editor, index)
                    )
                      event.preventDefault();
                  } else if (
                    event.key === "Backspace" &&
                    !event.ctrlKey &&
                    !event.metaKey &&
                    !event.altKey &&
                    event.currentTarget.selectionStart === 0 &&
                    event.currentTarget.selectionEnd === 0 &&
                    backspaceIntoPill(index)
                  ) {
                    event.preventDefault();
                  }
                }}
              />
            ),
          )}
          {/* Pinned to the first line; the group's right padding keeps wrapped
              pills and text from running under it. */}
          {onClear && (
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="Clear search"
              className="text-muted-foreground absolute top-1.5 right-1.5"
              onClick={() => {
                onClear();
                inputs.current.get(editor.pieces.length - 1)?.focus();
              }}
            >
              <XIcon aria-hidden="true" />
            </Button>
          )}
        </div>
        <Popover>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label="Search help"
            >
              <CircleHelpIcon aria-hidden="true" />
            </Button>
          </PopoverTrigger>
          <PopoverContent
            aria-label="User search help"
            className="space-y-2 text-sm"
          >
            <p>
              Search name, email, or organization with plain text. Use name:,
              email:, or org: to narrow a term. All terms must match.
            </p>
            <p>
              Quote phrases, for example org:"Example Studio". OR, negation,
              regex, and parentheses are not supported; quote them to search
              literally.
            </p>
            <p>
              Space or Enter creates a filter. Activate a filter to edit; Escape
              cancels. Backspace after a filter turns it back into text and
              keeps deleting. Undo and redo restore filter changes.
            </p>
          </PopoverContent>
        </Popover>
      </div>
      {/* The help text keeps its wrapped height under a shorter error, so the
          results below do not jump as the query flips between valid and not. */}
      <div className="text-muted-foreground grid text-xs">
        <p
          id={hintId}
          role="status"
          aria-live="polite"
          className="col-start-1 row-start-1"
        >
          {hint}
        </p>
        {hint !== help && (
          <p aria-hidden="true" className="invisible col-start-1 row-start-1">
            {help}
          </p>
        )}
      </div>
    </div>
  );
}
