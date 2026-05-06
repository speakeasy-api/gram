import { useState } from "react";
import { cn } from "@/lib/utils";

/**
 * Round avatar with a deterministic, gruvbox-tinted initials fallback. Used
 * for the current-user hero on the dashboard. Falls through to initials when
 * `src` is missing or the image fails to load.
 */
export function Avatar({
  src,
  name,
  fallback,
  size = "md",
  className,
}: {
  src?: string | null;
  name: string;
  fallback?: string;
  size?: "md" | "lg";
  className?: string;
}) {
  const [errored, setErrored] = useState(false);
  const initials = computeInitials(fallback ?? name);
  const dim = size === "lg" ? "size-14" : "size-10";

  if (src && !errored) {
    return (
      <img
        src={src}
        alt=""
        onError={() => setErrored(true)}
        className={cn(
          dim,
          "rounded-full object-cover ring-1 ring-border bg-muted shrink-0",
          className,
        )}
      />
    );
  }

  return (
    <div
      aria-hidden="true"
      className={cn(
        dim,
        "rounded-full flex items-center justify-center shrink-0",
        "bg-[var(--retro-yellow)]/25 text-foreground ring-1 ring-[var(--retro-yellow)]/50",
        "font-mono font-semibold uppercase tracking-wider",
        size === "lg" ? "text-base" : "text-sm",
        className,
      )}
    >
      {initials}
    </div>
  );
}

function computeInitials(s: string): string {
  const trimmed = s.trim();
  if (!trimmed) return "?";
  // Email — take first char and the char after the dot in the domain
  if (trimmed.includes("@")) {
    return trimmed[0]!.toUpperCase();
  }
  const parts = trimmed.split(/\s+/).filter(Boolean);
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}
