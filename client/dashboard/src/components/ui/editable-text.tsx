import { cn } from "@/lib/utils";
import { ReactNode, useState, useRef, useEffect } from "react";
import { Pencil } from "lucide-react";

interface EditableTextProps {
  value: string;
  onSubmit: (newValue: string) => void;
  renderDisplay: (value: string) => ReactNode;
  inputClassName?: string;
  validate?: (newValue: string) => boolean;
}

export function EditableText({
  value,
  onSubmit,
  renderDisplay,
  inputClassName,
  validate,
}: EditableTextProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [editedValue, setEditedValue] = useState(value);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        isEditing &&
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        handleSubmit();
      }
    };

    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [isEditing]);

  const handleStartEditing = (e: React.MouseEvent) => {
    e.stopPropagation();
    setEditedValue(value);
    setIsEditing(true);
  };

  const handleSubmit = () => {
    if (validate && !validate(editedValue)) {
      return;
    }
    setIsEditing(false);
    if (editedValue !== value) {
      onSubmit(editedValue);
    }
  };

  const PencilIcon = () => (
    <Pencil
      size={16}
      className={
        "text-muted-foreground opacity-0 group-hover:opacity-100 trans absolute left-[-18px] top-[50%] -translate-y-2/3"
      }
    />
  );

  const invalidClasses =
    validate && !validate(editedValue) ? "border-destructive" : "";

  return (
    <div
      ref={containerRef}
      onClick={handleStartEditing}
      className="group inline-flex items-center gap-2 cursor-pointer hover:opacity-80 relative w-fit"
    >
      <PencilIcon />
      <div
        className={cn(
          "absolute inset-0 pointer-events-none",
          isEditing ? "opacity-0" : "opacity-100"
        )}
      >
        {renderDisplay(value)}
      </div>
      <input
        ref={inputRef}
        type="text"
        value={editedValue}
        onChange={(e) => setEditedValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            handleSubmit();
          } else if (e.key === "Escape") {
            setIsEditing(false);
            setEditedValue(value);
          }
        }}
        className={cn(
          inputClassName,
          "outline-none text-thin! relative left-[-2px] w-full border-0 border-b border-dashed border-foreground/20",
          invalidClasses,
          isEditing
            ? "opacity-100 pointer-events-auto"
            : "opacity-0 pointer-events-none"
        )}
        disabled={!isEditing}
        autoFocus={isEditing}
        onBlur={handleSubmit}
      />
    </div>
  );
}
