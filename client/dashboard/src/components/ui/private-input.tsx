import { Input } from "@/components/ui/input";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";

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
}: PrivateInputProps) {
  const [isVisible, setIsVisible] = useState(false);

  return (
    <Input
      id={id}
      type={isVisible ? "text" : "password"}
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      className={className}
      readOnly={readOnly}
      disabled={disabled}
    >
      <button
        type="button"
        tabIndex={-1}
        onClick={() => setIsVisible(!isVisible)}
        className="text-muted-foreground hover:text-foreground absolute top-[6px] right-2 flex items-center justify-center transition-colors"
        disabled={disabled || readOnly}
      >
        {isVisible ? (
          <EyeOff className="h-4 w-4" />
        ) : (
          <Eye className="h-4 w-4" />
        )}
      </button>
    </Input>
  );
}
