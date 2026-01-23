import { Check, ChevronDown, Eye, EyeOff, Minus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";

interface Option {
  value: string;
  label: string;
}

interface InputAndMultiselectProps {
  value: string;
  onChange: (value: string) => void;
  onSelectedOptionsChange: (selected: string[]) => void;
  selectedOptions: string[];
  indeterminateOptions?: string[]; // Options that have different values in other contexts
  options: Option[];
  placeholder?: string;
  type?: "text" | "password";
  className?: string;
}

export function InputAndMultiselect({
  value,
  onChange,
  onSelectedOptionsChange,
  selectedOptions,
  indeterminateOptions = [],
  options,
  placeholder = "",
  type = "text",
  className,
}: InputAndMultiselectProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [showValue, setShowValue] = useState(false);
  const [dropdownPosition, setDropdownPosition] = useState({ top: 0, left: 0, width: 0 });
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Close on click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node) &&
        dropdownRef.current &&
        !dropdownRef.current.contains(event.target as Node)
      ) {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener("mousedown", handleClickOutside);
      return () => document.removeEventListener("mousedown", handleClickOutside);
    }
  }, [isOpen]);

  // Update dropdown position when opened or window resizes
  useEffect(() => {
    const updatePosition = () => {
      if (inputRef.current && isOpen) {
        const rect = inputRef.current.getBoundingClientRect();
        setDropdownPosition({
          top: rect.bottom + window.scrollY + 4,
          left: rect.left + window.scrollX,
          width: rect.width,
        });
      }
    };

    updatePosition();

    if (isOpen) {
      window.addEventListener("resize", updatePosition);
      window.addEventListener("scroll", updatePosition, true);
      return () => {
        window.removeEventListener("resize", updatePosition);
        window.removeEventListener("scroll", updatePosition, true);
      };
    }
  }, [isOpen]);

  const handleFocus = () => {
    setIsOpen(true);
  };

  const toggleOption = (optionValue: string) => {
    if (selectedOptions.includes(optionValue)) {
      onSelectedOptionsChange(selectedOptions.filter((v) => v !== optionValue));
    } else {
      onSelectedOptionsChange([...selectedOptions, optionValue]);
    }
  };

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <div className="relative">
        <input
          ref={inputRef}
          type={type === "password" && !showValue ? "password" : "text"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onFocus={handleFocus}
          placeholder={placeholder}
          className="w-full h-9 px-3 pr-9 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
        />
        {type === "password" && (
          <button
            onClick={() => setShowValue(!showValue)}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
            type="button"
          >
            {showValue ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          </button>
        )}
      </div>

      {/* Dropdown - rendered as portal to avoid clipping */}
      {isOpen &&
        createPortal(
          <div
            ref={dropdownRef}
            className="bg-popover border border-border rounded-md shadow-md z-100 p-1 max-h-[300px] overflow-y-auto"
            style={{
              position: "absolute",
              top: `${dropdownPosition.top}px`,
              left: `${dropdownPosition.left}px`,
              width: `${dropdownPosition.width}px`,
            }}
          >
            {options.map((option) => {
              const isSelected = selectedOptions.includes(option.value);
              const isIndeterminate = !isSelected && indeterminateOptions.includes(option.value);

              return (
                <div
                  key={option.value}
                  className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                  onClick={() => toggleOption(option.value)}
                >
                  <div
                    className={cn(
                      "w-4 h-4 rounded-sm border flex items-center justify-center",
                      isSelected
                        ? "bg-primary border-primary text-primary-foreground"
                        : isIndeterminate
                        ? "bg-muted border-border text-muted-foreground"
                        : "border-border",
                    )}
                  >
                    {isSelected && <Check className="h-3 w-3" />}
                    {isIndeterminate && <Minus className="h-3 w-3" />}
                  </div>
                  {option.label}
                </div>
              );
            })}
          </div>,
          document.body,
        )}
    </div>
  );
}
