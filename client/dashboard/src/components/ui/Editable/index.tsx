import { cn } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";
import { Text } from "@/components/ui/Text";

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
}): JSX.Element {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      className={cn("group relative cursor-pointer", className)}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onClick={() => {
        void (!disabled && onClick?.());
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
