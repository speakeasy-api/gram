import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { cn } from "@/lib/utils";
import { Check, Pencil } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";

type InlineEditableTextProps = {
  value: string;
  children: ReactNode;
  onSubmit: (value: string) => boolean | Promise<boolean>;
  inputLabel: string;
  editTitle: string;
  maxLength?: number;
  editorClassName?: string;
  inputClassName?: string;
  buttonClassName?: string;
  disabled?: boolean;
  normalizeValue?: (value: string) => string;
};

const trimValue = (value: string) => value.trim();

export function InlineEditableText({
  value,
  children,
  onSubmit,
  inputLabel,
  editTitle,
  maxLength,
  editorClassName,
  inputClassName,
  buttonClassName,
  disabled = false,
  normalizeValue = trimValue,
}: InlineEditableTextProps): JSX.Element {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const cancelNextBlur = useRef(false);
  const dirty = normalizeValue(draft) !== value;

  useEffect(() => {
    if (!editing) setDraft(value);
  }, [editing, value]);

  const submit = async () => {
    if (disabled || submittingRef.current) return;

    const nextValue = normalizeValue(draft);
    if (nextValue === value) {
      setEditing(false);
      return;
    }

    submittingRef.current = true;
    setSubmitting(true);
    try {
      const accepted = await onSubmit(nextValue);
      if (accepted) setEditing(false);
    } catch {
      // Submission feedback belongs to the caller; a failure keeps the draft open.
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  if (editing) {
    return (
      <InputGroup
        className={cn(
          "border-border bg-card dark:bg-card h-10 rounded-md rounded-tl-md rounded-tr-md rounded-br-md rounded-bl-md shadow-none has-[[data-slot=input-group-control]:focus-visible]:ring-1 has-[[data-slot=input-group-control]:focus-visible]:ring-ring/30",
          editorClassName,
        )}
      >
        <InputGroupInput
          aria-label={inputLabel}
          autoFocus
          className={inputClassName}
          disabled={disabled || submitting}
          maxLength={maxLength}
          onBlur={() => {
            if (cancelNextBlur.current) {
              cancelNextBlur.current = false;
              return;
            }
            void submit();
          }}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              event.currentTarget.blur();
            }
            if (event.key === "Escape") {
              cancelNextBlur.current = true;
              setDraft(value);
              setEditing(false);
            }
          }}
          value={draft}
        />
        {dirty && (
          <InputGroupAddon align="inline-end">
            <InputGroupButton
              aria-label="Save change"
              disabled={disabled || submitting}
              onClick={() => void submit()}
              onMouseDown={(event) => event.preventDefault()}
              size="icon-sm"
              title="Save change"
              variant="tertiary"
            >
              <Check />
            </InputGroupButton>
          </InputGroupAddon>
        )}
      </InputGroup>
    );
  }

  return (
    <button
      className={cn("group flex min-w-0 items-center gap-2", buttonClassName)}
      disabled={disabled}
      onClick={() => {
        cancelNextBlur.current = false;
        setEditing(true);
      }}
      title={editTitle}
      type="button"
    >
      {children}
      <Pencil className="text-muted-foreground h-4 w-4 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
    </button>
  );
}
