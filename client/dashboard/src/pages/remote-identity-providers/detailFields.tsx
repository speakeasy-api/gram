import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useRef, useState, type ReactNode } from "react";

// Shared read-only field primitives for the org-admin Remote Identity Provider
// and Remote Session Client detail Overview tabs: a small muted label above a
// left-aligned value, grouped under a section heading.

// InfoText is the default value style for an info field: small, breaking long
// values (URLs, joined lists) rather than overflowing. Pass `mono` for slugs,
// URLs, and other machine values.
export function InfoText({
  children,
  mono,
}: {
  children: ReactNode;
  mono?: boolean;
}): JSX.Element {
  return (
    <Text
      small
      as="div"
      className={mono ? "font-mono break-all" : "break-words"}
    >
      {children}
    </Text>
  );
}

// InfoField renders a small muted label above a left-aligned value.
// Pass `badges` to tag the label with short machine values that qualify the
// field, such as the authentication methods a token endpoint accepts.
export function InfoField({
  label,
  badges,
  children,
  className,
}: {
  label: string;
  badges?: string[] | null;
  children: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    // min-w-0 lets a one-line URL truncate inside a grid column instead of
    // widening it.
    <div
      data-info-field
      className={cn("flex min-w-0 flex-col gap-1", className)}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Text small muted>
          {label}
        </Text>
        {badges?.map((badge) => (
          <Badge
            key={badge}
            background={false}
            className="tracking-normal normal-case"
          >
            {badge}
          </Badge>
        ))}
      </div>
      {children}
    </div>
  );
}

// InfoSection is a titled group of fields stacked below a section heading.
export function InfoSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="min-w-0">
      <Heading variant="h4" className="mb-3">
        {title}
      </Heading>
      <div className="space-y-4">{children}</div>
    </div>
  );
}

// OverflowText keeps a long machine value (a URL, an issuer) on one line,
// truncated, and when it is cut off lays the whole value over it on hover. It
// follows the MCP server sidebar's URL row: the trigger and the card carry the
// same insets, so pulling the card up by the trigger's height lands the two
// texts on each other. The trigger takes focus so keyboard users can reveal
// the value too.
export function OverflowText({
  children,
  muted,
}: {
  children: string;
  muted?: boolean;
}): JSX.Element {
  const lineRef = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  // CSS can't tell when text is truncated, so check at the moment the card
  // would open: it only opens when the line is cut off.
  const onOpenChange = (next: boolean) => {
    const text = lineRef.current?.firstElementChild;
    setOpen(next && !!text && text.scrollWidth > text.clientWidth);
  };

  return (
    <div className="min-w-0">
      {/* No delay: this reveals text already on screen. */}
      <HoverCard openDelay={0} open={open} onOpenChange={onOpenChange}>
        <HoverCardTrigger asChild>
          <span
            ref={lineRef}
            tabIndex={0}
            className="focus-visible:ring-ring -mx-2 block px-2 py-1 focus-visible:ring-1 focus-visible:outline-none"
          >
            <Text
              small
              muted={muted}
              as="span"
              className="block truncate font-mono"
            >
              {children}
            </Text>
          </span>
        </HoverCardTrigger>
        <HoverCardContent
          align="start"
          side="bottom"
          sideOffset={-28}
          // A value wider than the viewport wraps instead of running off it.
          className="w-max max-w-[calc(100vw-2rem)] px-2 py-1 font-mono text-sm break-all duration-75"
        >
          {children}
        </HoverCardContent>
      </HoverCard>
    </div>
  );
}

// InfoUrl renders an endpoint or other URL value on one line, "—" when unset.
export function InfoUrl({ value }: { value: string | undefined }): JSX.Element {
  return value ? <OverflowText>{value}</OverflowText> : <InfoText>—</InfoText>;
}

// InfoList renders a metadata array as a comma-separated list, "—" when empty.
export function InfoList({
  values,
}: {
  values: string[] | null | undefined;
}): JSX.Element {
  return (
    <InfoText mono>
      {values && values.length > 0 ? values.join(", ") : "—"}
    </InfoText>
  );
}

// InfoSupported renders a capability flag.
export function InfoSupported({ value }: { value: boolean }): JSX.Element {
  return <InfoText>{value ? "Supported" : "Not supported"}</InfoText>;
}
