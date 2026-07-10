import { Button, Input, Stack } from "@/components/ui/moonshine";
import { Type } from "@/components/ui/type";
import { useState } from "react";
import { toastError } from "@/lib/toast-error";
import { Dialog } from "./ui/dialog";
import { Label } from "./ui/label";
import { ImageUpload } from "./upload";

type InputProps =
  | {
      type?: "text";
      label: string;
      placeholder: string;
      value: string;
      onChange: (value: string) => void;
      validate?: (value: string) => string | boolean;
      onSubmit?: (value: string) => void;
      optional?: boolean;
      disabled?: boolean;
      lines?: number;
      hint?: string | ((value: string) => React.ReactNode);
    }
  | {
      type: "image";
      label: string;
      value: string;
      onChange: (assetId: string) => void;
      onSubmit?: (assetId: string) => void;
      optional?: boolean;
      hint?: string | ((value: string) => React.ReactNode);
    };

// Moonshine's Input has no built-in `validate` prop (the old local Input did
// the inline error text/border itself), so InputDialog now derives the error
// state itself. Mirrors the old Input's v(): an empty value never shows an
// error (even if validate would reject it) — formValid below still gates
// submission on the raw validate() result independent of this display rule.
function inputErrorState(input: InputProps): {
  hasError: boolean;
  message: string | null;
} {
  if (input.type === "image" || input.value === "") {
    return { hasError: false, message: null };
  }
  const result = input.validate?.(input.value);
  if (result === false) return { hasError: true, message: null };
  if (typeof result === "string") return { hasError: true, message: result };
  return { hasError: false, message: null };
}

export function InputDialog({
  open,
  onOpenChange,
  onSubmit,
  title,
  description,
  inputs,
  submitButtonText = "Submit",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit?: () => void | Promise<void>;
  title: string;
  inputs: InputProps[] | InputProps;
  description?: string;
  submitButtonText?: string;
}): JSX.Element {
  const inputsArray = Array.isArray(inputs) ? inputs : [inputs];
  inputsArray.sort((a, b) => (a.optional ? 1 : b.optional ? -1 : 0));
  const [pending, setPending] = useState(false);

  const submit = async () => {
    if (pending) return;
    inputsArray.forEach((input) => {
      if (!input.optional || input.value !== "") {
        input.onSubmit?.(input.value);
      }
    });
    try {
      setPending(true);
      await onSubmit?.();
      onOpenChange(false);
    } catch (err) {
      toastError(err, "Something went wrong");
    } finally {
      setPending(false);
    }
  };

  const formValid =
    inputsArray.every((input) => {
      if (input.optional) {
        return true;
      }
      if (input.type === "image") {
        return input.value !== "";
      }
      return input.validate?.(input.value) ?? true;
    }) && inputsArray.some((input) => input.value !== "");

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!pending) onOpenChange(v);
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          {description && (
            <Dialog.Description>{description}</Dialog.Description>
          )}
        </Dialog.Header>
        <Stack gap={6} className="my-4">
          {inputsArray.map((input) => {
            const errorState = inputErrorState(input);
            return (
              <Stack key={input.label} gap={2}>
                <Label>
                  {input.label}
                  {input.optional && (
                    <span className="text-muted-foreground">(optional)</span>
                  )}
                </Label>
                {input.type !== "image" && (
                  <Input
                    placeholder={input.placeholder}
                    value={input.value}
                    onChange={(e) => input.onChange(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") void submit();
                    }}
                    disabled={input.disabled || pending}
                    error={errorState.hasError}
                    multiline={!!input.lines && input.lines > 1}
                  />
                )}
                {input.type === "image" && (
                  <ImageUpload
                    onUpload={(asset) => input.onChange(asset.id)}
                    existingAssetId={input.value}
                  />
                )}
                {input.type !== "image" &&
                  (errorState.message ? (
                    <Type variant="small" className="text-destructive! h-4">
                      {errorState.message}
                    </Type>
                  ) : (
                    <div className="h-[8px]" />
                  ))}
                {input.hint && (
                  <div className="text-muted-foreground text-sm">
                    {typeof input.hint === "function"
                      ? input.hint(input.value)
                      : input.hint}
                  </div>
                )}
              </Stack>
            );
          })}
        </Stack>
        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Back
          </Button>
          <Button
            onClick={() => void submit()}
            disabled={!formValid || pending}
          >
            {pending ? "Submitting…" : submitButtonText}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
