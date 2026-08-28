import { cva, type VariantProps } from "class-variance-authority";
import { Modifier, Variant } from "./types";
import { Icon } from "@/components/ui/Icon";
import { iconNames } from "../Icon/names";
import { useState } from "react";
import { cn } from "@/lib/utils";

const flexClasses = "flex flex-row gap-3";

const alertVariants = cva<{
  variant: {
    [k in Variant]: string;
  };
  modifiers: {
    [k in Modifier]: string;
  };
}>(
  `min-w-48 max-h-fit flex flex-row subpixel-antialiased font-light items-center px-3 pr-2 py-2 w-full border`,
  {
    variants: {
      variant: {
        default: "bg-card",
        success: "bg-card text-default-success border-success-default",
        error: "bg-card text-default-destructive border-destructive-default",
        warning: "bg-card text-default-warning border-warning-default",
        info: "bg-card text-default-information border-information-default",
        feature: "bg-card text-default-information border-information-default",
      },
      modifiers: {
        inline: "inline-flex",
      },
    },
  },
);

export type AlertProps = {
  variant?: NonNullable<VariantProps<typeof alertVariants>["variant"]>;
  children: React.ReactNode;
  inline?: boolean;
  dismissible?: boolean;
  onDismiss?: () => void;
  iconName?: (typeof iconNames)[number];
  useContainer?: boolean;
  className?: string;

  // alignTop pins the icon to the first line of the body instead of centering
  // it against the whole alert. Opt in for an alert whose body runs to several
  // lines, where a centered icon drifts into the middle of the text and stops
  // reading as a marker for the message.
  alignTop?: boolean;
};

const iconForVariant: Record<Variant, (typeof iconNames)[number] | undefined> =
  {
    default: "info",
    success: "check",
    error: "circle-alert",
    warning: "circle-alert",
    info: "info",
    feature: "star",
  };

export function Alert({
  variant = "default",
  children,
  inline = false,
  dismissible = false,
  onDismiss,
  iconName,
  useContainer = false,
  className,
  alignTop = false,
}: AlertProps): React.JSX.Element {
  const [isDismissing, setIsDismissing] = useState(false);
  const handleDismiss = () => {
    setIsDismissing(true);
    onDismiss?.();
  };
  const icon = iconName ?? iconForVariant[variant];
  const innerContent = (
    <div className={cn(flexClasses, alignTop ? "items-start" : "items-center")}>
      <div className="flex-shrink-0">
        {icon && <Icon name={icon} size="small" />}
      </div>
      <div>{children}</div>
    </div>
  );

  const dismissableContent = dismissible && (
    <div className="ml-auto self-start">
      <button
        type="button"
        aria-label="Dismiss"
        className="p-2 hover:bg-accent/10"
        onClick={handleDismiss}
      >
        <Icon name="x" />
      </button>
    </div>
  );

  return (
    <div
      role="alert"
      className={cn(
        alertVariants({ variant, modifiers: inline ? "inline" : undefined }),
        isDismissing && "opacity-0 transition-opacity duration-500",
        className,
      )}
    >
      {useContainer ? (
        <div className="container flex">
          {innerContent}
          {dismissableContent}
        </div>
      ) : (
        <>
          {innerContent}
          {dismissableContent}
        </>
      )}
    </div>
  );
}

/**
 * Title line for an alert body. Kept alongside `Alert` so callers can build a
 * titled alert without hand-rolling the type scale.
 */
export function AlertTitle({
  className,
  ...props
}: React.HTMLAttributes<HTMLHeadingElement>): React.JSX.Element {
  return (
    <h5
      className={cn("mb-1 leading-none font-medium tracking-tight", className)}
      {...props}
    />
  );
}

/** Body copy for an alert, sized to sit under {@link AlertTitle}. */
export function AlertDescription({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>): React.JSX.Element {
  return (
    <div
      className={cn("text-foreground text-sm [&_p]:leading-relaxed", className)}
      {...props}
    />
  );
}

export interface ErrorAlertProps {
  error: Error | string;
  title?: string;
  onDismiss?: () => void;
  className?: string;
}

/** An {@link Alert} pre-wired to render a thrown `Error` or an error string. */
export function ErrorAlert({
  error,
  title = "Error",
  onDismiss,
  className,
}: ErrorAlertProps): React.JSX.Element {
  return (
    <Alert
      variant="error"
      className={className}
      dismissible={Boolean(onDismiss)}
      onDismiss={onDismiss}
    >
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        {typeof error === "string" ? error : error.message}
      </AlertDescription>
    </Alert>
  );
}
