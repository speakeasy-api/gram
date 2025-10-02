import { cn } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";
import { Type } from "./type";

export function Editable({
  onClick,
  children,
  className,
  disabled,
}: {
  onClick?: () => void;
  className?: string;
  children: React.ReactNode;
  disabled?: boolean;
}) {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      className={cn("relative group cursor-pointer", className)}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onClick={() => !disabled && onClick?.()}
    >
      <div
        className={`transition-all duration-200 ${isHovered ? "blur-xs" : ""}`}
      >
        {children}
      </div>
      {isHovered && (
        <div className="absolute inset-0 flex items-center justify-center">
          {disabled ? (
            <Type muted italic>
              Can't edit
            </Type>
          ) : (
            <>
              <Pencil className="w-4 h-4 text-muted-foreground mr-1" />
              <Type
                className={cn(
                  "font-medium text-inherit",
                  disabled && "text-muted-foreground",
                )}
              >
                Edit
              </Type>
            </>
          )}
        </div>
      )}
    </div>
  );
}
