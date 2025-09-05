import { Stack } from "@speakeasy-api/moonshine";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Input } from "./ui/input";
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
  onSubmit?: () => void;
  title: string;
  inputs: InputProps[] | InputProps;
  description?: string;
  submitButtonText?: string;
}) {
  const inputsArray = Array.isArray(inputs) ? inputs : [inputs];
  inputsArray.sort((a, b) => (a.optional ? 1 : b.optional ? -1 : 0));

  const submit = () => {
    inputsArray.forEach((input) => {
      if (!input.optional || input.value !== "") {
        input.onSubmit?.(input.value);
      }
    });
    onSubmit?.();
    onOpenChange(false);
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          {description && (
            <Dialog.Description>{description}</Dialog.Description>
          )}
        </Dialog.Header>
        <Stack gap={6} className="my-4">
          {inputsArray.map((input) => (
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
                  onChange={input.onChange}
                  onEnter={submit}
                  disabled={input.disabled}
                  validate={input.validate}
                  lines={input.lines}
                />
              )}
              {input.type === "image" && (
                <ImageUpload
                  onUpload={(asset) => input.onChange(asset.id)}
                  existingAssetId={input.value}
                />
              )}
              {input.hint && (
                <div className="text-sm text-muted-foreground">
                  {typeof input.hint === "function"
                    ? input.hint(input.value)
                    : input.hint}
                </div>
              )}
            </Stack>
          ))}
        </Stack>
        <Dialog.Footer>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Back
          </Button>
          <Button onClick={submit} disabled={!formValid}>
            {submitButtonText}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
