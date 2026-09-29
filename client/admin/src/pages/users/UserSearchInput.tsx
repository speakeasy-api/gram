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
}: UserSearchInputProps): React.JSX.Element {
  const hintId = useId();
  const [editor, setEditor] = useState(() => tokenize(value));
  const [seenValue, setSeenValue] = useState(value);
  const lastEmitted = useRef<string | undefined>(undefined);
  const undo = useRef<Change[]>([]);
  const redo = useRef<Change[]>([]);
  const composing = useRef(false);
  const inputs = useRef(new Map<number, HTMLInputElement>());
  const buttons = useRef(new Map<number, HTMLButtonElement>());
  const focus = useRef<number | null>(null);
  const [selected, setSelected] = useState<number | null>(null);

  // A synchronous parent's echo must not turn the current edit back into a pill.
  // Genuine URL restores repaint immediately and cannot undo into another URL.
  if (value !== seenValue) {
    setSeenValue(value);
    if (value !== lastEmitted.current) {
      setEditor(tokenize(value));
      setSelected(null);
      undo.current = [];
      redo.current = [];
      focus.current = null;
    }
    lastEmitted.current = undefined;
  }

  useLayoutEffect(() => {
    if (focus.current === null) return;
    inputs.current.get(focus.current)?.focus();
    focus.current = null;
  }, [editor]);

  function update(
    next: Editor,
    transformation = false,
    focusIndex?: number,
    settleEdit = false,
  ) {
    let before = editor;
    if (settleEdit && editor.editing) {
      const original = editor.editing.original;
      const settle = (state: Editor) =>
        state.editing?.original === original ? restoreEdit(state) : state;
      // Keep intervening removals as separate operations while settling this
      // edit. The edit opener becomes a no-op; removal snapshots retain their
      // own before/after pieces with the original filter restored.
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
    setSelected(null);
    if (focusIndex !== undefined) focus.current = focusIndex;
    const query = queryOf(next);
    if (query !== queryOf(editor)) {
      lastEmitted.current = query;
      onChange(query);
    }
  }

  function remove(index: number) {
    const pieces = editor.pieces.filter((_, i) => i !== index);
    if (pieces.at(-1)?.term || pieces.length === 0) pieces.push({ text: "" });
    const editing = editor.editing
      ? {
          ...editor.editing,
          index: editor.editing.index - (index < editor.editing.index ? 1 : 0),
        }
      : undefined;
    update(
      { pieces, ...(editing ? { editing } : {}) },
      true,
      pieces.length - 1,
    );
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
    setSelected(null);
    focus.current = next.editing?.index ?? next.pieces.length - 1;
    lastEmitted.current = queryOf(next);
    onChange(queryOf(next));
  }

  const parsed = parseUserSearch(queryOf(editor));
  const selectedTerm =
    selected === null ? undefined : editor.pieces[selected]?.term;
  const hint =
    error ||
    (!parsed.ok
      ? parsed.message
      : selectedTerm
        ? `${selectedTerm.field} filter selected: ${selectedTerm.value}. Press Backspace to remove or Enter to edit.`
        : "Use name:, email:, or org:. Space or Enter creates a filter; Escape cancels editing.");

  return (
    <div className="min-w-0 space-y-1">
      <div
        className="flex min-w-0 flex-wrap items-center gap-2"
        onKeyDown={history}
      >
        <SearchIcon
          aria-hidden="true"
          className="text-muted-foreground size-4 shrink-0"
        />
        {editor.pieces.map((piece, index) =>
          piece.term ? (
            <Badge
              key={`filter-${index}`}
              variant="secondary"
              className="max-w-full gap-0"
              data-selected={selected === index || undefined}
            >
              <Button
                ref={(node) => {
                  if (node) buttons.current.set(index, node);
                  else buttons.current.delete(index);
                }}
                type="button"
                variant="ghost"
                size="xs"
                className="min-w-0 whitespace-normal break-all"
                aria-label={`Edit ${piece.term.field} filter: ${piece.term.value}`}
                aria-describedby={hintId}
                onBlur={() => setSelected(null)}
                onKeyDown={(event) => {
                  if (composing.current || event.nativeEvent.isComposing)
                    return;
                  if (event.key === "Backspace" && selected === index) {
                    event.preventDefault();
                    remove(index);
                  }
                  if (event.key === "Escape") {
                    setSelected(null);
                    inputs.current.get(editor.pieces.length - 1)?.focus();
                  }
                }}
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
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Remove ${piece.term.field} filter: ${piece.term.value}`}
                onClick={() => remove(index)}
              >
                <XIcon aria-hidden="true" />
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
              className="min-w-32 flex-1 basis-40"
              placeholder="Search users…"
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
                    event.currentTarget.selectionStart === piece.text.length &&
                    event.currentTarget.selectionEnd === piece.text.length &&
                    commit(editor, index)
                  )
                    event.preventDefault();
                } else if (event.key === "Backspace" && piece.text === "") {
                  const finalPill = editor.pieces.findLastIndex(
                    (p) => p.term !== undefined,
                  );
                  if (finalPill >= 0) {
                    event.preventDefault();
                    setSelected(finalPill);
                    buttons.current.get(finalPill)?.focus();
                  }
                }
              }}
            />
          ),
        )}
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
              cancels. In an empty input, Backspace selects the last filter;
              Backspace again removes it. Undo and redo restore filter changes.
            </p>
          </PopoverContent>
        </Popover>
      </div>
      <p
        id={hintId}
        role="status"
        aria-live="polite"
        className="text-muted-foreground text-xs"
      >
        {hint}
      </p>
    </div>
  );
}
