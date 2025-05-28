import { useState } from "react";
import { Type } from "./type";
import { Pencil } from "lucide-react";

export function Editable({
  onClick,
  children,
}: {
  onClick?: () => void;
  children: React.ReactNode;
}) {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      className="relative group cursor-pointer"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onClick={() => onClick?.()}
    >
      <div
        className={`transition-all duration-200 ${isHovered ? "blur-xs" : ""}`}
      >
        {children}
      </div>

      {isHovered && (
        <div className="absolute inset-0 flex items-center justify-center">
          <Pencil className="w-4 h-4 text-muted-foreground mr-1" />
          <Type className="font-medium text-inherit">
            Edit
          </Type>
        </div>
      )}
    </div>
  );
}
