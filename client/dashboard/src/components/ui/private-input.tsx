import { Input } from "@/components/ui/input";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/utils";

interface PrivateInputProps {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  readOnly?: boolean;
  disabled?: boolean;
}

export function PrivateInput({
  id,
  value,
  onChange,
  placeholder,
  className,
  readOnly,
  disabled,
}: PrivateInputProps): JSX.Element {
  const [isVisible, setIsVisible] = useState(false);

  return (
    <div className={cn("relative", className)}>
      <Input
        id={id}
        type={isVisible ? "text" : "password"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="pr-9"
        readOnly={readOnly}
        disabled={disabled}
      />
      <button
        type="button"
        tabIndex={-1}
        onClick={() => setIsVisible(!isVisible)}
        className="text-muted hover:text-highlight absolute top-1/2 right-2 flex -translate-y-1/2 items-center justify-center transition-colors"
        disabled={disabled || readOnly}
      >
        {isVisible ? (
          <EyeOff className="h-4 w-4" />
        ) : (
          <Eye className="h-4 w-4" />
        )}
      </button>
    </div>
  );
}
