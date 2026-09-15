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

// columnClasses is a static lookup rather than an interpolated class name,
// because Tailwind extracts classes by scanning source text and would not emit a
// class built at runtime.
const columnClasses = {
  2: "sm:grid-cols-2",
  3: "sm:grid-cols-3",
} as const;

// InfoFieldGrid lays short fields out side by side instead of giving each one a
// full-width stacked block, which is what turns a handful of scalar values into
// a long scroll.
//
// Long machine values (resource names, service account emails, URLs) belong
// outside it: they wrap badly in a narrow column, so they read better left at
// full width.
export function InfoFieldGrid({
  children,
  columns = 3,
}: {
  children: ReactNode;
  columns?: keyof typeof columnClasses;
}): JSX.Element {
  return (
    <div className={`grid gap-4 ${columnClasses[columns]}`}>{children}</div>
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
