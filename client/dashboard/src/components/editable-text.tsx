import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { Pencil } from "lucide-react";
import { ReactNode, useEffect, useState } from "react";
import { InputDialog } from "./input-dialog";

interface EditableTextProps {
  label: string;
  value: string | undefined;
  onSubmit: (newValue: string) => void;
  validate?: (newValue: string) => boolean;
  children: ReactNode;
}

export function EditableText({
  label,
  value,
  onSubmit,
  validate,
  children,
}: EditableTextProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [editedValue, setEditedValue] = useState(value);

  const handleSubmit = () => {
    if (!editedValue) {
      return;
    }

    if (validate && !validate(editedValue)) {
      return;
    }
    if (editedValue !== value) {
      onSubmit(editedValue);
    }
  };

  useEffect(() => {
    if (!editedValue) {
      setEditedValue(value);
    }
  }, [value]);

  return (
    <>
      <div
        onClick={() => setIsEditing(true)}
        className={cn("group cursor-pointer hover:opacity-80")}
      >
        <Stack direction="horizontal" align="center" gap={1}>
          <Pencil
            size={16}
            className={
              "text-muted-foreground opacity-0 group-hover:opacity-100 trans ml-[-18px]"
            }
          />
          {children}
        </Stack>
      </div>
      <InputDialog
        open={isEditing}
        onOpenChange={setIsEditing}
        title={`Edit ${label}`}
        description={`Update the value of ${label.toLowerCase()}`}
        submitButtonText="Update"
        inputs={{
          label: label,
          placeholder: label,
          value: editedValue ?? "Loading...",
          onChange: setEditedValue,
          onSubmit: handleSubmit,
          validate: validate ?? (() => true),
        }}
      />
    </>
  );
}
