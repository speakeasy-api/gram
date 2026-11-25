import type { ComponentProps, ReactNode } from "react";

import { Button } from "@/components/ui/Button";

type ButtonProps = ComponentProps<typeof Button>;

/** A button that opens an already-validated Okta console URL in a new tab. */
export function OktaLinkButton({
  href,
  variant,
  size = "sm",
  tooltip,
  "aria-label": ariaLabel,
  children,
}: {
  href: string;
  variant: ButtonProps["variant"];
  size?: ButtonProps["size"];
  tooltip?: string;
  "aria-label"?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Button asChild variant={variant} size={size} tooltip={tooltip}>
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={ariaLabel}
      >
        {children}
      </a>
    </Button>
  );
}
