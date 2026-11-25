import { useIdentityTint } from "@/components/gradient-colors";
import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import { formatDistanceToNow } from "date-fns/formatDistanceToNow";
import { useState } from "react";
import type { Identity, Mark, Status, Tone, Value } from "./types";

const TONE_TEXT: Record<Tone, string> = {
  neutral: "text-muted-foreground",
  success: "text-default-success",
  warning: "text-default-warning",
  destructive: "text-default-destructive",
  information: "text-default-information",
};

const TONE_DOT: Record<Tone, string> = {
  neutral: "bg-muted-foreground",
  success: "bg-[var(--fill-success-default)]",
  warning: "bg-[var(--fill-warning-default)]",
  destructive: "bg-[var(--fill-destructive-default)]",
  information: "bg-[var(--fill-information-default)]",
};

/** A dot plus a word: color is never the only signal. */
export function StatusText({ status }: { status: Status }): JSX.Element {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 text-sm",
        TONE_TEXT[status.tone],
      )}
    >
      <span
        aria-hidden="true"
        className={cn("size-1.5 shrink-0", TONE_DOT[status.tone])}
      />
      {status.label}
    </span>
  );
}

function Tile({
  label,
  iconUrl,
  size,
  className,
}: {
  label: string;
  iconUrl?: string;
  size: number;
  className?: string;
}): JSX.Element {
  const tint = useIdentityTint(label);
  const initial = label.trim().charAt(0).toUpperCase() || "?";
  // Logos come from external registries: load only HTTPS, and fall back to
  // the initial when the image fails.
  const [broken, setBroken] = useState<string | null>(null);
  const src =
    iconUrl?.startsWith("https://") && broken !== iconUrl ? iconUrl : undefined;
  return (
    <span
      aria-hidden="true"
      className={cn(
        "bg-card flex shrink-0 items-center justify-center overflow-hidden border",
        className,
      )}
      style={{
        width: size,
        height: size,
        ...(src ? {} : tint),
        fontSize: Math.round(size * 0.44),
      }}
    >
      {src ? (
        <img
          src={src}
          alt=""
          onError={() => setBroken(src)}
          className="size-full object-contain p-[12%]"
        />
      ) : (
        initial
      )}
    </span>
  );
}

export function MarkTile({
  mark,
  size,
}: {
  mark: Mark;
  size: number;
}): JSX.Element {
  const sub = Math.round(size * 0.46);
  return (
    <span className="relative shrink-0" style={{ width: size, height: size }}>
      <Tile label={mark.label} iconUrl={mark.iconUrl} size={size} />
      {mark.secondary && (
        // Overhangs the corner so a gateway reads as "fronts this".
        <span
          className="ring-card absolute ring-2"
          style={{ left: size - sub + 4, top: size - sub + 4 }}
        >
          <Tile
            label={mark.secondary.label}
            iconUrl={mark.secondary.iconUrl}
            size={sub}
          />
        </span>
      )}
      {mark.secondary && (
        <span className="sr-only">Includes {mark.secondary.label}</span>
      )}
    </span>
  );
}

/** Identity cell: mark, name, one line of context. Truncates; full text on hover. */
export function IdentityCell({
  identity,
  size = 32,
}: {
  identity: Identity;
  size?: number;
}): JSX.Element {
  return (
    <span className="flex min-w-0 items-center gap-3">
      <MarkTile mark={identity.mark} size={size} />
      <span className="flex min-w-0 flex-col">
        <span className="text-foreground truncate" title={identity.title}>
          {identity.title}
        </span>
        {identity.subtitle && (
          <span
            className="text-muted-foreground truncate text-sm"
            title={identity.subtitle}
          >
            {identity.subtitle}
          </span>
        )}
      </span>
    </span>
  );
}

export function ValueView({ value }: { value: Value }): JSX.Element {
  switch (value.kind) {
    case "text":
      return (
        <span
          className={cn("truncate", value.mono && "font-mono text-sm")}
          title={value.text}
        >
          {value.text}
        </span>
      );
    case "number": {
      const unit = value.unit
        ? ` ${value.value === 1 ? value.unit.one : value.unit.many}`
        : "";
      return (
        <span className="font-mono text-sm tabular-nums">
          {value.value.toLocaleString()}
          {unit}
        </span>
      );
    }
    case "status":
      return <StatusText status={value.status} />;
    case "tag":
      return (
        <Badge variant={value.tone ?? "neutral"} size="md">
          {value.text}
        </Badge>
      );
    case "date":
      return (
        <time
          dateTime={value.date.toISOString()}
          title={value.date.toLocaleString()}
          className="text-muted-foreground text-sm"
        >
          {formatDistanceToNow(value.date, { addSuffix: true })}
        </time>
      );
    case "link":
      return (
        <a
          href={value.href}
          target={value.external ? "_blank" : undefined}
          rel={value.external ? "noreferrer" : undefined}
          className="truncate underline underline-offset-2"
          title={value.href}
        >
          {value.text}
        </a>
      );
    case "empty":
      return <span className="text-muted-foreground">{value.text}</span>;
  }
}
