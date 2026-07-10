import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva } from "class-variance-authority";

import { cn } from "@/components/ui/moonshine/lib/utils";
import {
  ButtonSize,
  ButtonVariant,
  ButtonContext,
} from "@/components/ui/moonshine/types";

// Lerp for angles, taking the shortest path around the circle
const lerpAngle = (a: number, b: number, t: number) => {
  // Normalize angles to 0-360 range
  a = ((a % 360) + 360) % 360;
  b = ((b % 360) + 360) % 360;

  // Calculate the shortest difference
  let diff = b - a;
  if (diff > 180) {
    diff -= 360;
  } else if (diff < -180) {
    diff += 360;
  }

  // Interpolate and normalize result
  const result = a + diff * t;
  return ((result % 360) + 360) % 360;
};

const useAnimationFrame = (
  callback: (timestamp: number, delta: number) => void,
  enabled = true,
) => {
  const requestRef = React.useRef<number | undefined>(undefined);
  const previousTimeRef = React.useRef<number | undefined>(undefined);

  const animate = React.useCallback(
    (timestamp: number) => {
      if (previousTimeRef.current !== undefined) {
        const deltaTime = timestamp - previousTimeRef.current;
        callback(timestamp, deltaTime);
      }
      previousTimeRef.current = timestamp;
      if (enabled) {
        requestRef.current = requestAnimationFrame(animate);
      }
    },
    [callback, enabled],
  );

  React.useEffect(() => {
    if (enabled) {
      requestRef.current = requestAnimationFrame(animate);
    }
    return () => {
      if (requestRef.current) {
        cancelAnimationFrame(requestRef.current);
      }
    };
  }, [animate, enabled]);
};

const ButtonLeftIcon = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      "inline-flex shrink-0 items-center justify-center",
      className,
    )}
    {...props}
  />
));
ButtonLeftIcon.displayName = "ButtonLeftIcon";

const ButtonRightIcon = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      "inline-flex shrink-0 items-center justify-center",
      className,
    )}
    {...props}
  />
));
ButtonRightIcon.displayName = "ButtonRightIcon";

const ButtonIcon = ButtonLeftIcon;

const ButtonText = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn("flex-1 text-trim-cap", className)}
    {...props}
  />
));
ButtonText.displayName = "ButtonText";

const BrandSlot = React.forwardRef<
  HTMLElement,
  React.ComponentProps<typeof Slot> & { isBrandVariant?: boolean }
>(({ isBrandVariant = false, ...slotProps }, slotRef) => {
  if (!isBrandVariant) {
    return <Slot {...slotProps} ref={slotRef} />;
  }

  const child = React.Children.only(slotProps.children);
  if (!React.isValidElement(child)) {
    return <Slot {...slotProps} ref={slotRef} />;
  }

  const brandSpan = (
    <span
      key="brand-bg"
      className="pointer-events-none absolute inset-0 -z-10 bg-btn-brand hover:bg-btn-brand-hover disabled:bg-btn-brand-disabled"
    />
  );

  const typedChild = child as React.ReactElement<
    React.ComponentProps<typeof Slot>
  >;

  const childWithBrand = React.cloneElement(typedChild, {
    children: typedChild.props.children
      ? [brandSpan, typedChild.props.children]
      : brandSpan,
  });

  return (
    <Slot {...slotProps} ref={slotRef}>
      {childWithBrand}
    </Slot>
  );
});
BrandSlot.displayName = "BrandSlot";

const buttonVariants = cva(
  "relative inline-flex items-center justify-center whitespace-nowrap text-sm font-sans font-light tracking-[0.01em] transition-all select-none cursor-pointer focus-visible:outline-none focus-visible:ring-0 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-[var(--bg-surface-primary-default)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      context: {
        product: "rounded-sm",
        marketing: "rounded-full",
      },
      variant: {
        brand:
          'relative text-btn-brand hover:text-btn-brand-hover disabled:text-btn-brand-disabled [transform:translateZ(0)] before:absolute before:content-[""] before:-z-10 before:pointer-events-none [--gradient-rotation:220deg] before:bg-[conic-gradient(from_var(--gradient-rotation),hsl(334,54%,13%),hsl(4,67%,47%),hsl(23,96%,62%),hsl(68,52%,72%),hsl(108,24%,41%),hsl(154,100%,7%),hsl(220,100%,12%),hsl(214,69%,50%),hsl(216,100%,80%),hsl(334,54%,13%))] after:absolute after:content-[""] after:-z-20 after:pointer-events-none after:opacity-0 after:transition-opacity after:duration-300 hover:after:opacity-100 after:bg-[conic-gradient(from_var(--gradient-rotation),hsl(334,54%,13%),hsl(4,67%,47%),hsl(23,96%,62%),hsl(68,52%,72%),hsl(108,24%,41%),hsl(154,100%,7%),hsl(220,100%,12%),hsl(214,69%,50%),hsl(216,100%,80%),hsl(334,54%,13%))] after:blur-[2px] focus-visible:ring-2 focus-visible:ring-offset-3 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-[var(--bg-surface-primary-default)]',
        primary:
          "bg-btn-primary text-btn-primary hover:bg-btn-primary-hover hover:text-btn-primary-hover active:bg-btn-primary-active active:text-btn-primary-active disabled:bg-btn-primary-disabled disabled:text-btn-primary-disabled",
        secondary:
          "border border-neutral-active bg-transparent text-btn-secondary hover:border-neutral-hover hover:bg-btn-secondary-hover hover:text-btn-secondary-hover active:bg-btn-secondary-active active:text-btn-secondary-active disabled:border-neutral-disabled disabled:bg-transparent disabled:text-btn-secondary-disabled",
        tertiary:
          "bg-transparent text-btn-tertiary hover:bg-btn-secondary-hover hover:text-btn-tertiary-hover active:bg-btn-secondary-active active:text-btn-tertiary-active disabled:text-btn-tertiary-disabled",
        "destructive-primary":
          "bg-btn-destructive text-btn-destructive-primary hover:bg-btn-destructive-hover hover:text-btn-destructive-primary-hover active:bg-btn-destructive-active active:text-btn-destructive-primary-active disabled:bg-btn-destructive-disabled disabled:text-btn-destructive-primary-disabled",
        "destructive-secondary":
          "bg-transparent text-btn-destructive-secondary hover:text-btn-destructive-secondary-hover active:text-btn-destructive-secondary-active disabled:text-btn-destructive-secondary-disabled",
      },
      size: {
        xs: "h-7 px-2 py-1 text-xs gap-1 [&_svg]:size-3",
        sm: "h-8 px-3 py-2 text-sm gap-1.5 [&_svg]:size-3.5",
        md: "h-9 px-4 py-2 gap-2 [&_svg]:size-4",
        lg: "h-10 px-6 py-2 text-lg gap-2.5 [&_svg]:size-5",
      },
    },
    compoundVariants: [
      {
        variant: "brand",
        className:
          "before:inset-[-1px] active:before:inset-[-2px] after:inset-[-1px] active:after:inset-[-2px]",
      },
      {
        context: "marketing",
        variant: "brand",
        className: "before:rounded-full after:rounded-full",
      },
    ],
    defaultVariants: {
      context: "product",
      variant: "primary",
      size: "md",
    },
  },
);

type Attributes = Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "style">;

export interface ButtonProps extends Attributes {
  asChild?: boolean;
  variant?: ButtonVariant;
  size?: ButtonSize;
  context?: ButtonContext;
  className?: string;
  "aria-label"?: string;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      variant = "primary",
      size = "md",
      context = "product",
      asChild = false,
      className,
      onMouseEnter,
      onMouseLeave,
      onMouseDown,
      onMouseUp,
      onMouseMove,
      ...props
    },
    ref,
  ) => {
    const [cursorPosition, setCursorPosition] = React.useState<{
      x: number;
      y: number;
    } | null>(null);
    const buttonRef = React.useRef<HTMLButtonElement | null>(null);
    const buttonDimensionsRef = React.useRef<{
      width: number;
      height: number;
    } | null>(null);

    if (process.env.NODE_ENV === "development" && !asChild) {
      const validateChildren = () => {
        if (!props.children) return;

        const childArray = React.Children.toArray(props.children);
        const hasButtonText = childArray.some(
          (child) =>
            (typeof child === "string" && child.trim().length > 0) ||
            typeof child === "number" ||
            (React.isValidElement(child) &&
              (child.type === ButtonText ||
                (child.type as { displayName?: string })?.displayName ===
                  "ButtonText")),
        );
        const hasAriaLabel = props["aria-label"];

        if (!hasButtonText && !hasAriaLabel) {
          console.warn(
            "Button: Buttons should either contain Button.Text or have an aria-label for accessibility. Consider using IconButton for icon-only buttons.",
          );
        }

        const invalidChildren = childArray.filter((child) => {
          if (typeof child === "string" || typeof child === "number") {
            return false; // Raw text is OK - we'll auto-wrap it
          }
          if (React.isValidElement(child)) {
            const displayName = (child.type as { displayName?: string })
              ?.displayName;
            return ![
              "ButtonText",
              "ButtonLeftIcon",
              "ButtonRightIcon",
              "ButtonIcon",
            ].includes(displayName || "");
          }
          return true; // Other types are invalid
        });

        if (invalidChildren.length > 0) {
          console.warn(
            "Button: Only Button.Text, Button.LeftIcon, Button.RightIcon, and raw text should be used as children.",
          );
        }
      };

      validateChildren();
    }

    const gradientRefs = React.useRef<{
      target: number;
      current: number;
    }>({
      target: 220,
      current: 220,
    });

    const isBrandVariant = variant === "brand";

    // Get gap class for the current size
    const getGapClass = (size: ButtonSize) => {
      switch (size) {
        case "xs":
          return "gap-1";
        case "sm":
          return "gap-1.5";
        case "md":
          return "gap-2";
        case "lg":
          return "gap-2.5";
        default:
          return "gap-2";
      }
    };

    // Only run animation frame when brand variant is active
    useAnimationFrame(
      React.useCallback(
        (_timestamp: number, delta: number) => {
          if (!buttonRef.current) return;

          const gradientValues = gradientRefs.current;

          // Calculate target based on cursor position if hovering
          if (cursorPosition && buttonDimensionsRef.current) {
            const centerX = buttonDimensionsRef.current.width / 2;
            const centerY = buttonDimensionsRef.current.height / 2;

            // Calculate angle from center to cursor position
            const angle =
              Math.atan2(
                cursorPosition.y - centerY,
                cursorPosition.x - centerX,
              ) *
              (180 / Math.PI);

            // Normalize angle to 0-360 range and add offset
            gradientValues.target = (angle + 360) % 360;
          } else {
            // Always reset to original position when not hovering
            gradientValues.target = 220;
          }

          // Early exit if we're close enough to target
          const diff = Math.abs(gradientValues.target - gradientValues.current);
          if (diff < 0.1) {
            if (diff > 0) {
              gradientValues.current = gradientValues.target;
              buttonRef.current.style.setProperty(
                "--gradient-rotation",
                `${Math.round(gradientValues.target)}deg`,
              );
            }
            return;
          }

          const lerpValue = Math.min((delta / 1000) * 8, 1); // Increased speed for snappier response

          const newCurrent = lerpAngle(
            gradientValues.current,
            gradientValues.target,
            lerpValue,
          );
          gradientValues.current = newCurrent;

          buttonRef.current.style.setProperty(
            "--gradient-rotation",
            `${Math.round(newCurrent)}deg`,
          );
        },
        [cursorPosition],
      ),
      isBrandVariant, // Only enable animation for brand variant
    );

    if (process.env.NODE_ENV === "development") {
      const deprecatedVariants = {
        default: "primary",
        destructive: "destructive-primary",
        outline: "secondary",
        ghost: "tertiary",
        link: "a semantic <a> tag",
      };

      if (variant && variant in deprecatedVariants) {
        console.warn(
          `Button: The variant "${variant}" is deprecated. Please use "${deprecatedVariants[variant as keyof typeof deprecatedVariants]}" instead.`,
        );
      }
    }

    const handleMouseMove = React.useCallback(
      (e: React.MouseEvent<HTMLButtonElement>) => {
        if (isBrandVariant && buttonRef.current) {
          const rect = buttonRef.current.getBoundingClientRect();
          const x = e.clientX - rect.left;
          const y = e.clientY - rect.top;
          setCursorPosition({ x, y });
        }
        onMouseMove?.(e);
      },
      [isBrandVariant, onMouseMove],
    );

    const handleMouseEnter = React.useCallback(
      (e: React.MouseEvent<HTMLButtonElement>) => {
        if (isBrandVariant && buttonRef.current) {
          const rect = buttonRef.current.getBoundingClientRect();
          // Cache dimensions for animation frame
          buttonDimensionsRef.current = {
            width: rect.width,
            height: rect.height,
          };
          const x = e.clientX - rect.left;
          const y = e.clientY - rect.top;
          setCursorPosition({ x, y });
        }
        onMouseEnter?.(e);
      },
      [isBrandVariant, onMouseEnter],
    );

    const handleMouseLeave = React.useCallback(
      (e: React.MouseEvent<HTMLButtonElement>) => {
        if (isBrandVariant) {
          setCursorPosition(null);
          buttonDimensionsRef.current = null;
          // Explicitly set target to ensure reset
          gradientRefs.current.target = 220;
        }
        onMouseLeave?.(e);
      },
      [isBrandVariant, onMouseLeave],
    );

    const handleMouseDown = React.useCallback(
      (e: React.MouseEvent<HTMLButtonElement>) => {
        onMouseDown?.(e);
      },
      [onMouseDown],
    );

    const handleMouseUp = React.useCallback(
      (e: React.MouseEvent<HTMLButtonElement>) => {
        onMouseUp?.(e);
      },
      [onMouseUp],
    );

    React.useEffect(() => {
      if (isBrandVariant && buttonRef.current) {
        buttonRef.current.style.setProperty("--gradient-rotation", "220deg");
        Object.assign(gradientRefs.current, { target: 220, current: 220 });
      }
    }, [isBrandVariant]);

    const Comp = asChild ? BrandSlot : "button";
    const combinedRef = React.useCallback(
      (node: HTMLButtonElement | null) => {
        buttonRef.current = node;
        if (typeof ref === "function") {
          ref(node);
        } else if (ref && node) {
          (ref as React.MutableRefObject<HTMLButtonElement | null>).current =
            node;
        }
      },
      [ref],
    );

    // Auto-wrap raw text children in Button.Text (only when not using asChild)
    const processedChildren = React.useMemo(() => {
      if (asChild) {
        // When asChild is true, return children as-is - BrandSlot will handle brand logic
        return props.children;
      }

      return React.Children.map(props.children, (child) => {
        if (typeof child === "string" || typeof child === "number") {
          return <ButtonText>{child}</ButtonText>;
        }
        return child;
      });
    }, [props.children, asChild]);

    return (
      <Comp
        className={cn(buttonVariants({ variant, size, context }), className)}
        ref={combinedRef}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        onMouseMove={handleMouseMove}
        onMouseDown={handleMouseDown}
        onMouseUp={handleMouseUp}
        {...(asChild ? { isBrandVariant } : {})}
        {...props}
      >
        {asChild ? (
          processedChildren
        ) : (
          <>
            {isBrandVariant && (
              <span className="pointer-events-none absolute inset-0 z-10 bg-btn-brand hover:bg-btn-brand-hover disabled:bg-btn-brand-disabled" />
            )}
            <span
              className={cn(
                "relative flex items-center justify-center",
                getGapClass(size),
                isBrandVariant ? `z-20` : "",
              )}
            >
              {processedChildren}
            </span>
          </>
        )}
      </Comp>
    );
  },
);
Button.displayName = "Button";

const ButtonWithCompounds = Object.assign(Button, {
  Icon: ButtonIcon,
  LeftIcon: ButtonLeftIcon,
  RightIcon: ButtonRightIcon,
  Text: ButtonText,
});

export { ButtonWithCompounds as Button, ButtonIcon };
