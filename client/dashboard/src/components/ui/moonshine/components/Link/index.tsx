import React, { forwardRef, ReactNode } from "react";
import { cn } from "@/components/ui/moonshine/lib/utils";
import { cva } from "class-variance-authority";
import { Slot, Slottable } from "@radix-ui/react-slot";
import { IconName } from "../Icon/names";
import { Icon as IconComponent } from "../Icon";
import { TextVariant } from "../Text";

type LinkVariant = "primary" | "secondary";

const linkVariants = cva(
  "group/link items-center inline-flex flex-row cursor-pointer",
  {
    variants: {
      variant: {
        primary: "text-link-primary visited:text-link-visited",
        secondary: "text-link-secondary visited:text-link-visited",
      },
      size: {
        xs: "text-body-xs gap-1",
        sm: "text-body-sm gap-1",
        md: "text-body-md gap-2",
        lg: "text-body-lg gap-2",
      },
    },
  },
);

const linkTextVariants = cva(
  "underline-offset-4 decoration-1 inline-flex flex-row items-center",
  {
    variants: {
      variant: {
        primary: "",
        secondary: "",
      },
      underline: {
        false: "no-underline group-hover/link:underline",
        true: "underline group-hover/link:no-underline",
      },
    },
  },
);

const iconWrapperVariants = cva("inline-block", {
  variants: {
    size: {
      xs: "size-3 [&>svg]:size-3",
      sm: "size-3 [&>svg]:size-3",
      md: "size-4 [&>svg]:size-4",
      lg: "size-4 [&>svg]:size-4",
    },
  },
});

export interface LinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  children: ReactNode;
  variant?: LinkVariant;
  size?: TextVariant;
  underline?: boolean;
  iconPrefixName?: IconName;
  iconSuffixName?: IconName;
  asChild?: boolean;
}

export const Link = forwardRef<HTMLAnchorElement, LinkProps>(
  (
    {
      children,
      variant = "primary",
      size = "md",
      underline = true,
      target = "_blank",
      iconPrefixName,
      iconSuffixName,
      className,
      asChild = false,
      ...rest
    },
    ref,
  ) => {
    const Comp = asChild ? Slot : "a";

    // When asChild is true, we need to apply underline styles to the main component
    // since the Text wrapper won't be used
    const underlineClasses = asChild
      ? linkTextVariants({ underline, variant })
      : "";

    return (
      <Comp
        ref={ref}
        target={target}
        className={cn(
          linkVariants({ size, variant }),
          underlineClasses,
          className,
        )}
        {...rest}
      >
        {iconPrefixName && (
          <IconWrapper size={size}>
            <IconComponent name={iconPrefixName} size="small" />
          </IconWrapper>
        )}

        {asChild ? (
          <Slottable>{children}</Slottable>
        ) : (
          <Text underline={underline} variant={variant}>
            {children}
          </Text>
        )}

        {iconSuffixName && (
          <IconWrapper size={size}>
            <IconComponent name={iconSuffixName} size="small" />
          </IconWrapper>
        )}
      </Comp>
    );
  },
);
Link.displayName = "Link";

interface TextProps {
  underline?: boolean;
  variant?: LinkVariant;
}

const Text = ({
  children,
  underline,
  variant,
}: React.PropsWithChildren<TextProps>) => {
  return (
    <span className={cn(linkTextVariants({ underline, variant }))}>
      {children}
    </span>
  );
};

interface IconWrapperProps {
  size?: TextVariant;
}

const IconWrapper = ({
  children,
  size,
}: React.PropsWithChildren<IconWrapperProps>) => {
  return <span className={cn(iconWrapperVariants({ size }))}>{children}</span>;
};
