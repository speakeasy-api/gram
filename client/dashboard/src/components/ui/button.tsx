import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { Icon, IconName } from "@speakeasy-api/moonshine";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 shrink-0 [&_svg]:shrink-0 outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground hover:bg-primary/90 hover:ring-primary/20 hover:ring-2",
        destructive:
          "bg-destructive text-white hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/60",
        destructiveGhost:
          "text-muted-foreground hover:text-foreground hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 ",
        outline:
          "border bg-background hover:bg-accent hover:text-accent-foreground dark:bg-input/30 dark:border-input dark:hover:bg-input/50",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-secondary/80 border",
        ghost:
          "hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2 has-[>svg]:px-3",
        inline: "h-7 rounded-sm px-1.5 py-1.5 gap-0.5",
        sm: "h-8 rounded-md gap-1.5 px-3 has-[>svg]:px-2.5",
        lg: "h-10 rounded-md px-6 has-[>svg]:px-4",
        icon: "size-9",
        "icon-sm": "size-8",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export function Button({
  className,
  variant,
  size,
  tooltip,
  icon: iconName,
  caps = false,
  iconAfter = false,
  asChild = false,
  ...props
}: React.ComponentProps<"button"> & React.ComponentProps<"a"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
    tooltip?: string;
    icon?: IconName;
    iconAfter?: boolean;
    caps?: boolean;
    href?: string;
  }) {
  const Comp: React.ElementType = asChild ? Slot : (props.href ? "a" : "button");

  const iconColors = {
    default: "text-primary-foreground/60 group-hover:text-primary-foreground",
    destructive: "text-destructive/80 group-hover:text-foreground",
    destructiveGhost: "text-muted-foreground group-hover:text-foreground",
    outline: "text-muted-foreground group-hover:text-foreground",
    secondary: "text-muted-foreground group-hover:text-foreground",
    ghost: "text-muted-foreground group-hover:text-foreground",
    link: "text-muted-foreground group-hover:text-foreground",
  }[variant ?? "default"];

  const icon = iconName && (
    <Icon
      name={iconName}
      className={cn(
        "w-4 h-4 text-muted-foreground group-hover:text-foreground",
        iconColors
      )}
    />
  );

  const onClick = props.onClick
    ? (e: React.MouseEvent<HTMLButtonElement>) => {
        // Stop propagation generally, but these elements break without it
        if (props["aria-haspopup"] !== "dialog") {
          e.stopPropagation();
          e.preventDefault();
        }
        props.onClick?.(e);
      }
    : undefined;

  // We do it like this because certain Comps need a single child
  let childrenWithIcons = props.children;
  if (icon && !iconAfter) {
    childrenWithIcons = (
      <>
        {icon}
        {props.children}
      </>
    );
  } else if (icon && iconAfter) {
    childrenWithIcons = (
      <>
        {props.children}
        {icon}
      </>
    );
  }

  const base = (
    <Comp
      data-slot="button"
      className={cn(
        buttonVariants({ variant, size, className }),
        "cursor-pointer group trans",
        caps && "uppercase font-mono"
      )}
      {...props}
      {...(onClick ? { onClick } : {})}
    >
      {childrenWithIcons}
    </Comp>
  );

  if (tooltip) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            {/* div is necessary to retain the tooltip when the button is disabled */}
            <div>{base}</div>
          </TooltipTrigger>
          <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return base;
}
