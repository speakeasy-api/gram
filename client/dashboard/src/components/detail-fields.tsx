import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import type { ReactNode } from "react";

// Shared read-only field primitives for detail-page Overview tabs: a small muted
// label above a left-aligned value, grouped under a section heading, with no
// surrounding box. Mirrors the look of the Remote Identity Provider detail
// fields so detail pages read consistently across the dashboard.

// InfoText is the default value style for an info field: small, breaking long
// values (URLs, joined lists) rather than overflowing. Pass `mono` for slugs,
// resource names, and other machine values.
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
export function InfoField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <Text small muted>
        {label}
      </Text>
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
    <div>
      <Heading variant="h4" className="mb-3">
        {title}
      </Heading>
      <div className="space-y-4">{children}</div>
    </div>
  );
}
