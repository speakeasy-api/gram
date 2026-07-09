import { cn } from "@/components/ui/moonshine/lib/utils";
import { ReactNode } from "react";

export type TextVariant = "lg" | "md" | "sm" | "xs";
type TextElement = "p" | "span" | "div" | "label";
type TextWhitespace = "normal" | "nowrap";

export type TextProps = {
  children: ReactNode;
  variant?: TextVariant;
  muted?: boolean;
  whiteSpace?: TextWhitespace;
  className?: string;
} & (
  | {
      as?: Exclude<TextElement, "label">;
      htmlFor?: never;
    }
  | {
      as: "label";
      htmlFor?: string;
    }
);

const variantStyles: Record<TextVariant, string> = {
  lg: "text-body-lg",
  md: "text-body-md",
  sm: "text-body-sm",
  xs: "text-body-xs",
};

const whitespaceStyles: Record<TextWhitespace, string> = {
  normal: "whitespace-normal",
  nowrap: "whitespace-nowrap",
};

export function Text({
  children,
  variant = "md",
  as: Component = "span",
  muted = false,
  whiteSpace = "normal",
  className,
  htmlFor,
}: TextProps): React.JSX.Element {
  return (
    <Component
      htmlFor={htmlFor}
      className={cn(
        variantStyles[variant],
        whitespaceStyles[whiteSpace],
        muted ? "text-muted" : "text-default",
        className,
      )}
    >
      {children}
    </Component>
  );
}
