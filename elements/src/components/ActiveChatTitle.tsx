import { useAssistantApi, useAssistantState } from "@assistant-ui/react";
import { PencilIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import {
  FALLBACK_TITLE,
  MAX_TITLE_LENGTH,
  resolveTitleEdit,
} from "@/components/activeChatTitle.helpers";

export interface ActiveChatTitleProps {
  className?: string;
}

/**
 * Inline-editable title for the active conversation, intended for a chat
 * header. Reads the active thread's title from the assistant-ui runtime and
 * saves edits through `threadListItem().rename`, which optimistically updates
 * the runtime and calls the Gram thread-list adapter (→ chat.generateTitle).
 *
 * Renaming requires a persisted thread (a remote id). A brand-new conversation
 * only has a local id until its first message, so the title renders as a
 * read-only "New Chat" until then. Clearing the title (saving empty) resets it
 * to automatic, session-context naming.
 *
 * Must be rendered inside an Elements runtime provider.
 */
export function ActiveChatTitle({
  className,
}: ActiveChatTitleProps): React.JSX.Element {
  const api = useAssistantApi();
  const title = useAssistantState((s) => s.threadListItem.title);
  const remoteId = useAssistantState((s) => s.threadListItem.remoteId);

  const persisted = Boolean(remoteId);
  const currentTitle = title?.trim() ?? "";
  const displayTitle = currentTitle || FALLBACK_TITLE;

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  // Escape/Enter both blur the input; this flag stops the blur handler from
  // double-handling (or, after Escape, from saving the discarded draft).
  const skipBlurRef = useRef(false);

  useEffect(() => {
    if (editing) {
      inputRef.current?.select();
    }
  }, [editing]);

  // If the active thread switches while editing, abandon the in-progress draft.
  // Without this, a pending edit (or a blur-save fired during the switch) would
  // apply the previous thread's draft to the newly active thread.
  useEffect(() => {
    setEditing(false);
    skipBlurRef.current = true;
  }, [remoteId]);

  const startEditing = () => {
    if (!persisted) return;
    // Reset the blur guard so this fresh edit session saves on blur as normal
    // (it may still be set from an Enter/Escape, or from a thread switch).
    skipBlurRef.current = false;
    setDraft(title ?? "");
    setEditing(true);
  };

  const finishEditing = (save: boolean) => {
    setEditing(false);
    if (!save) return;
    const { changed, value } = resolveTitleEdit(draft, currentTitle);
    if (!changed) return;
    api.threadListItem().rename(value);
  };

  if (editing) {
    return (
      <input
        ref={inputRef}
        value={draft}
        maxLength={MAX_TITLE_LENGTH}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            skipBlurRef.current = true;
            finishEditing(true);
          } else if (e.key === "Escape") {
            e.preventDefault();
            skipBlurRef.current = true;
            finishEditing(false);
          }
        }}
        onBlur={() => {
          if (skipBlurRef.current) {
            skipBlurRef.current = false;
            return;
          }
          finishEditing(true);
        }}
        aria-label="Chat title"
        className={cn(
          "min-w-0 rounded border bg-transparent px-1.5 py-0.5 text-sm font-medium text-foreground outline-none focus-visible:ring-1",
          className,
        )}
      />
    );
  }

  // `group` lets the pencil reveal on hover/focus of the whole title region, so
  // the rename affordance is discoverable instead of relying on a subtle
  // background change. The pencil only renders for a persisted thread — a
  // brand-new conversation has nothing to rename until its first message.
  return (
    <div className={cn("group flex min-w-0 items-center gap-0.5", className)}>
      <button
        type="button"
        onClick={startEditing}
        disabled={!persisted}
        title={persisted ? "Rename conversation" : undefined}
        className="min-w-0 truncate rounded px-1.5 py-0.5 text-left text-sm font-medium text-foreground hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
      >
        {displayTitle}
      </button>
      {persisted && (
        <button
          type="button"
          onClick={startEditing}
          title="Rename conversation"
          aria-label="Rename conversation"
          className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:bg-muted hover:text-foreground focus-visible:opacity-100"
        >
          <PencilIcon className="size-3.5" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
