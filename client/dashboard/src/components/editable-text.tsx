import { ReactNode, useEffect, useState } from "react";
import { InputDialog } from "./input-dialog";
import { Editable } from "./ui/editable";

interface EditableTextProps {
  label: string;
  description?: string;
  value: string | undefined;
  onSubmit: (newValue: string) => void | Promise<void>;
  validate?: (newValue: string) => string | boolean;
  lines?: number;
  disabled?: boolean;
  placeholder?: string;
  children: ReactNode;
}

export function EditableText({
  label,
  description,
  value,
  onSubmit,
  validate,
  lines,
  disabled,
  placeholder = label,
  children,
}: EditableTextProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [editedValue, setEditedValue] = useState(value);

  const handleSubmit = async () => {
    if (!editedValue) {
      return;
    }

    if (validate && !validate(editedValue)) {
      return;
    }
    if (editedValue !== value) {
      await onSubmit(editedValue);
    }
  };

  useEffect(() => {
    setEditedValue(value);
  }, [value]);

  const handleOpenChange = (open: boolean) => {
    // When the dialog is closed, reset the edited value to the original value
    if (!open) {
      setEditedValue(value);
    }
    setIsEditing(open);
  };

  return (
    <>
      <Editable
        disabled={disabled}
        onClick={() => handleOpenChange(true)}
        className="w-fit"
      >
        {children}
      </Editable>
      <InputDialog
        open={isEditing}
        onOpenChange={handleOpenChange}
        title={`Edit ${label}`}
        description={description}
        submitButtonText="Update"
        inputs={{
          label,
          placeholder,
          value: editedValue ?? (!placeholder ? "Loading..." : ""),
          disabled,
          onChange: setEditedValue,
          onSubmit: handleSubmit,
          validate: validate ?? (() => true),
          lines,
        }}
      />
    </>
  );
}
