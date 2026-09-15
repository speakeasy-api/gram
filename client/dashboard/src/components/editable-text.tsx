import { ReactNode, useEffect, useState } from "react";
import { InputDialog } from "./input-dialog";
import { cn } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { Text } from "@/components/ui/Text";

/**
 * Hover-to-edit overlay: blurs its children on hover and floats a "Edit" affordance
 * (or a "Can't edit" note when disabled). Local to EditableText — its only user.
 */
function Editable({
  onClick,
  children,
  className,
  disabled,
}: {
  onClick?: () => void;
  className?: string;
  children: ReactNode;
  disabled?: boolean;
}): JSX.Element {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      role="button"
      tabIndex={disabled ? -1 : 0}
      aria-disabled={disabled}
      className={cn("group relative", !disabled && "cursor-pointer", className)}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onClick={() => {
        void (!disabled && onClick?.());
      }}
      onKeyDown={(event) => {
        if (!disabled && (event.key === "Enter" || event.key === " ")) {
          event.preventDefault();
          onClick?.();
        }
      }}
    >
      <div
        className={`transition-all duration-200 ${isHovered ? "blur-xs" : ""}`}
      >
        {children}
      </div>
      {isHovered && (
        <div className="absolute inset-0 flex items-center justify-center">
          {disabled ? (
            <Text muted italic>
              Can't edit
            </Text>
          ) : (
            <>
              <Pencil className="text-muted-foreground mr-1 h-4 w-4" />
              <Text
                className={cn(
                  "font-medium text-inherit",
                  disabled && "text-muted-foreground",
                )}
              >
                Edit
              </Text>
            </>
          )}
        </div>
      )}
    </div>
  );
}

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
}: EditableTextProps): JSX.Element {
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
          onSubmit: () => void handleSubmit(),
          validate: validate ?? (() => true),
          lines,
        }}
      />
    </>
  );
}
