import * as React from "react";
import { Button, ButtonIcon } from "../Button";
import {
  ButtonVariant,
  ButtonSize,
  ButtonContext,
} from "@/components/ui/moonshine/types";
import { cn } from "@/components/ui/moonshine/lib/utils";

type Attributes = Pick<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  | "disabled"
  | "onClick"
  | "type"
  | "role"
  | "onMouseEnter"
  | "onMouseLeave"
  | "onMouseDown"
  | "onMouseUp"
>;

interface IconButtonProps extends Attributes {
  icon: React.ReactNode;
  variant?: ButtonVariant;
  size?: ButtonSize;
  context?: ButtonContext;
  className?: string;
  asChild?: boolean;
  "aria-label": string; // Required for accessibility
}

const IconButton = React.forwardRef<HTMLButtonElement, IconButtonProps>(
  ({ icon, size = "md", className, ...props }, ref) => {
    // Map button sizes to square dimensions with proper icon sizing
    const sizeClasses = {
      xs: "h-7 w-7 [&_svg]:size-3",
      sm: "h-8 w-8 [&_svg]:size-3.5",
      md: "h-9 w-9 [&_svg]:size-4",
      lg: "h-10 w-10 [&_svg]:size-5",
    };

    return (
      <Button
        ref={ref}
        className={cn(sizeClasses[size], "p-0", className)}
        {...props}
      >
        <ButtonIcon>{icon}</ButtonIcon>
      </Button>
    );
  },
);
IconButton.displayName = "IconButton";

export { IconButton };
