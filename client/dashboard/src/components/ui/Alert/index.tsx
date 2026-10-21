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
  // `text-sm` because an alert is body copy, not a heading: at the inherited
  // base size it outweighed the content it sits above. Callers that patched
  // this with their own `text-sm` no longer need to.
  `min-w-48 max-h-fit flex flex-row subpixel-antialiased font-light items-start px-3 pr-2 py-2 w-full border text-sm`,
  {
    variants: {
      variant: {
        default: "bg-card",
        success: "bg-card text-default-success border-success-default",
        error: "bg-card text-default-destructive border-destructive-default",
        warning: "bg-card text-default-warning border-warning-default",
        info: "bg-card text-default-information border-information-default",
        // Violet, not the information blue it used to borrow: a feature
        // notice is not a severity, and sharing blue made it read as one.
        feature: "bg-card text-default-feature border-feature-default",
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
  /**
   * The severity line above the body. Defaults to the variant's own word;
   * pass a string to say something more specific, or `null` where the body
   * already names what happened and a label would only repeat it.
   */
  title?: React.ReactNode | null;
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

/**
 * What each variant calls itself. Every variant has a word, so an alert
 * always opens by saying how much to care: the colour alone carries that for
 * some readers and not for others, and a bare sentence in a box says nothing
 * at all to a screen reader beyond its text.
 */
const titleForVariant: Record<Variant, string> = {
  // Not "Information", which is the `info` variant's word: this one is the
  // unstyled fallback, and a reader seeing both at once should be able to
  // tell them apart.
  default: "Note",
  success: "Success",
  error: "Error",
  warning: "Warning",
  info: "Information",
  feature: "Feature",
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
  title,
}: AlertProps): React.JSX.Element {
  const [isDismissing, setIsDismissing] = useState(false);
  const handleDismiss = () => {
    setIsDismissing(true);
    onDismiss?.();
  };
  const icon = iconName ?? iconForVariant[variant];
  // `undefined` takes the variant's word; `null` is an explicit "no label".
  const label = title === undefined ? titleForVariant[variant] : title;
  const innerContent = (
    <div className={cn(flexClasses, "items-start")}>
      {/* A box one text line tall, with the icon centred in it, so the icon
          sits on the first line whether the body is one line or six.
          Centring the icon against the whole alert instead drifted it into
          the middle of a long message, and left it optically high on a
          short one, because a glyph is shorter than its line box. */}
      <div className="flex h-5 flex-shrink-0 items-center">
        {icon && <Icon name={icon} size="small" />}
      </div>
      <div className="min-w-0 flex-1">
        {label !== null && (
          // The eyebrow the rest of the dashboard already uses for a marker
          // above a block of text. A severity label is exactly that, so it
          // borrows the idiom rather than inventing a second one. The line
          // box still matches the body's, so the icon lands on it.
          <div className="font-mono text-[10px] leading-5 tracking-[0.08em] uppercase">
            {label}
          </div>
        )}
        {/* The body drops to the foreground colour: the variant's colour is
            spent on the label and the border, and a whole paragraph in it
            reads as emphasis on every word rather than on the severity. */}
        <div className={cn(label !== null && "text-foreground mt-0.5")}>
          {children}
        </div>
      </div>
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
