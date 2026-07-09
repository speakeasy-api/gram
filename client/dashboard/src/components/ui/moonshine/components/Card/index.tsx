import { cn } from "@/components/ui/moonshine/lib/utils";
import React, { FC, PropsWithChildren, ReactNode } from "react";
import { Icon } from "../Icon";
import { Stack } from "../Stack";
import { Button } from "../Button";
import { Score } from "../Score";
import { iconNames } from "../Icon/names";
import { Children } from "react";
import { Range } from "@/components/ui/moonshine/lib/typeUtils";

type RightElement =
  | {
      type: "button";
      label: string;
      onClick: () => void;
    }
  | {
      type: "gauge";
      value: Range<100>;
    };

type IconProps = {
  name: (typeof iconNames)[number];
  size?: "small" | "medium" | "large";
};

type CardHeaderProps = PropsWithChildren & {
  subheader?: React.ReactNode;
  icon?: IconProps;
  rightElement?: RightElement;
  className?: string;
};

const CardHeader: FC<CardHeaderProps> = ({
  children,
  subheader,
  icon,
  rightElement,
  className,
}) => (
  <div
    className={cn(
      "flex w-full flex-row gap-4",
      subheader ? "items-start" : "items-center",
      className,
    )}
  >
    {icon && (
      <div className="flex-shrink-0 rounded-[8px] border p-2">
        <Icon name={icon.name} size={icon.size} />
      </div>
    )}

    <div className="flex min-w-0 flex-grow flex-col gap-1">
      <div className="text-md leading-none font-semibold tracking-tight">
        {children}
      </div>
      {subheader && (
        <div className="mt-1 flex items-center text-sm text-card-foreground">
          {subheader}
        </div>
      )}
    </div>

    {rightElement && (
      <div className="flex flex-shrink-0 justify-end gap-2">
        {rightElement.type === "button" && (
          <Button onClick={rightElement.onClick} variant="secondary">
            {rightElement.label}
          </Button>
        )}
        {rightElement.type === "gauge" && rightElement.value && (
          <Score score={rightElement.value} size="small" />
        )}
      </div>
    )}
  </div>
);
CardHeader.displayName = "CardHeader";

interface CardContentProps extends PropsWithChildren {
  className?: string;
}

const CardContent: FC<CardContentProps> = ({ children, className }) => (
  <div className={cn("text-sm", className)}>{children}</div>
);
CardContent.displayName = "CardContent";

type FooterContent = {
  text: string;
  link?: {
    label: string;
    href: string;
  };
};

type CardFooterProps = {
  content: FooterContent;
  className?: string;
};

const CardFooter: FC<CardFooterProps> = ({ content, className }) => (
  <div className={cn("border-t px-6 py-4", className)}>
    <div className="flex items-center text-sm text-card-foreground">
      {content.text}
      {content.link && (
        <a
          href={content.link.href}
          className="ml-2 text-primary hover:underline"
        >
          {content.link.label}
        </a>
      )}
    </div>
  </div>
);
CardFooter.displayName = "CardFooter";

type CardProps = {
  children: ReactNode | ReactNode[];
  onClick?: () => void;
  href?: string;
  className?: string;
};

const Card: FC<CardProps> = ({ children, onClick, href, className }) => {
  const validChildren = Children.toArray(children).filter(
    (child) =>
      React.isValidElement(child) &&
      (child.type === CardHeader ||
        child.type === CardContent ||
        child.type === CardFooter),
  );

  const hasButtonElement = Children.toArray(validChildren).some((child) => {
    if (
      React.isValidElement<CardHeaderProps>(child) &&
      child.type === CardHeader
    ) {
      return child.props.rightElement?.type === "button";
    }
    return false;
  });

  if (hasButtonElement && (onClick || href)) {
    console.warn(
      "Card: Card-level interaction (onClick/href) will be ignored when header contains a button element. " +
        "This prevents confusing UX with nested clickable elements.",
    );
  }

  const isInteractive = !hasButtonElement && Boolean(onClick || href);
  const Wrapper = href && !hasButtonElement ? "a" : "div";
  const wrapperProps = !hasButtonElement
    ? href
      ? { href }
      : onClick
        ? { onClick }
        : {}
    : {};

  return (
    <Wrapper
      className={cn(
        "relative flex h-full w-full flex-col rounded-[8px] border bg-card text-card-foreground shadow",
        isInteractive && "cursor-pointer hover:bg-card/70",
        className,
      )}
      {...wrapperProps}
    >
      <div className="p-6">
        <Stack gap={3}>
          {validChildren.map((child) => {
            if (React.isValidElement(child) && child.type === CardFooter) {
              return null;
            }

            return child;
          })}
        </Stack>
      </div>
      {validChildren.find(
        (child) => React.isValidElement(child) && child.type === CardFooter,
      )}
    </Wrapper>
  );
};

const CardWithSubcomponents = Object.assign(Card, {
  Header: CardHeader,
  Content: CardContent,
  Footer: CardFooter,
});

export { CardWithSubcomponents as Card };
