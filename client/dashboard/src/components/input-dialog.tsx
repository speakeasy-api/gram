import { Stack } from "@speakeasy-api/moonshine";
import { Dialog } from "./ui/dialog";
import { Input } from "./ui/input";
import { Button } from "./ui/button";
import { Label } from "./ui/label";

type InputProps = {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  validate?: (value: string) => boolean;
  onSubmit?: (value: string) => void;
  submitButtonText?: string;
  optional?: boolean;
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
  description: string;
  inputs: InputProps[] | InputProps;
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
      return input.validate?.(input.value) ?? false;
    }) && inputsArray.some((input) => input.value !== "");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
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
              <Input
                placeholder={input.placeholder}
                value={input.value}
                onChange={(e) => input.onChange(e.target.value)}
                onEnter={submit}
              />
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
