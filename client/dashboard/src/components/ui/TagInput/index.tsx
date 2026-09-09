import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import { XIcon } from "lucide-react";
import {
  useState,
  type ClipboardEvent,
  type FocusEvent,
  type KeyboardEvent,
} from "react";

export interface TagInputProps {
  /** Id of the inner text input, so a label's htmlFor can target it. */
  id?: string;
  /** The tags currently entered. */
  value: string[];
  /** Called with the new tag list whenever a tag is added or removed. */
  onChange: (value: string[]) => void;
  /** Shown in the input while no tags are entered. */
  placeholder?: string;
  /** Forces the error border. */
  error?: boolean;
  /**
   * Also turns typed text into a tag on space. Only for values that can never
   * contain a space, such as command names.
   */
  separateOnSpace?: boolean;
  disabled?: boolean;
  /** Extra classes for the outer field. */
  className?: string;
}

// splitTagText turns typed or pasted text into tags: split on commas and
// newlines (and whitespace when asked), trimmed, blanks dropped.
export function splitTagText(text: string, onSpace = false): string[] {
  return text
    .split(onSpace ? /[,\s]/ : /[,\n]/)
    .map((part) => part.trim())
    .filter((part) => part !== "");
}

// TagInput is a text field whose entries become removable chips: a comma,
// Enter, or Tab (and space when separateOnSpace is set) turns the text typed
// so far into a tag, Backspace on an empty input removes the last one, pasted
// lists are split the same way, and text still pending when the field loses
// focus is added rather than lost.
export function TagInput({
  id,
  value,
  onChange,
  placeholder,
  error,
  separateOnSpace = false,
  disabled,
  className,
}: TagInputProps): JSX.Element {
  const [draft, setDraft] = useState("");

  const add = (text: string): void => {
    const next = [...value];
    for (const tag of splitTagText(text, separateOnSpace)) {
      if (!next.includes(tag)) next.push(tag);
    }
    if (next.length !== value.length) onChange(next);
    setDraft("");
  };

  const removeAt = (index: number): void => {
    onChange(value.filter((_, i) => i !== index));
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>): void => {
    const isSeparator =
      event.key === "," || (separateOnSpace && event.key === " ");
    if (isSeparator || event.key === "Enter" || event.key === "Tab") {
      if (draft.trim() === "") {
        // An empty separator is noise; an empty Enter or Tab keeps its default
        // so the form can submit or focus can move on.
        if (isSeparator) event.preventDefault();
        return;
      }
      event.preventDefault();
      add(draft);
      return;
    }
    if (event.key === "Backspace" && draft === "" && value.length > 0) {
      removeAt(value.length - 1);
    }
  };

  const handlePaste = (event: ClipboardEvent<HTMLInputElement>): void => {
    const text = event.clipboardData.getData("text");
    if (!(separateOnSpace ? /[,\s]/ : /[,\n]/).test(text)) return;
    event.preventDefault();
    add(draft + text);
  };

  const handleBlur = (_event: FocusEvent<HTMLInputElement>): void => {
    if (draft.trim() !== "") add(draft);
  };

  return (
    <div
      className={cn(
        "flex min-h-9 flex-wrap items-center gap-1.5 border border-input bg-surface-primary-default px-3 py-1.5 focus-within:border-focus",
        error && "border-destructive-default",
        disabled && "cursor-not-allowed opacity-50",
        className,
      )}
    >
      {value.map((tag, index) => (
        <Badge key={tag} variant="neutral" className="max-w-full normal-case">
          <Badge.Text className="min-w-0 truncate font-mono">{tag}</Badge.Text>
          <Badge.RightIcon>
            <button
              type="button"
              disabled={disabled}
              onClick={() => removeAt(index)}
              aria-label={`Remove ${tag}`}
              className="flex h-3 w-3 cursor-pointer items-center justify-center hover:opacity-70 focus:outline-none focus-visible:ring-1"
            >
              <XIcon className="h-3 w-3" />
            </button>
          </Badge.RightIcon>
        </Badge>
      ))}
      <input
        id={id}
        type="text"
        value={draft}
        disabled={disabled}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        onBlur={handleBlur}
        placeholder={value.length === 0 ? placeholder : undefined}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        className="h-6 min-w-32 flex-1 bg-transparent font-mono text-sm text-default outline-none placeholder:text-placeholder disabled:cursor-not-allowed"
      />
    </div>
  );
}
