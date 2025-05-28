import { ReactNode, useEffect, useState } from "react";
import { InputDialog } from "./input-dialog";
import { Editable } from "./ui/editable";

interface EditableTextProps {
  label: string;
  description?: string;
  value: string | undefined;
  onSubmit: (newValue: string) => void;
  validate?: (newValue: string) => string | boolean;
  children: ReactNode;
}

export function EditableText({
  label,
  description,
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
      <Editable onClick={() => setIsEditing(true)}>{children}</Editable>
      <InputDialog
        open={isEditing}
        onOpenChange={setIsEditing}
        title={`Edit ${label}`}
        description={description}
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
