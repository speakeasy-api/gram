import { CheckIcon, CopyIcon, XIcon } from "lucide-react";
import { useEffect, useRef, useState, type JSX, type ReactNode } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { cn } from "@/lib/utils";

const COPY_CONFIRM_MS = 1500;

function noop(): void {}

function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString();
}

function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </>
  );
}

function CopyValue({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  return (
    <span className="flex items-center gap-1">
      <span className="truncate font-mono text-xs">{value}</span>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={copied ? `${label} copied` : `Copy ${label}`}
        onClick={() => {
          // A check over a failed write sends the operator off with the wrong id.
          void navigator.clipboard.writeText(value).then(() => {
            setCopied(true);
            clearTimeout(timer.current);
            timer.current = setTimeout(() => setCopied(false), COPY_CONFIRM_MS);
          }, noop);
        }}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    </span>
  );
}

export function PeekPanel({
  org,
  onClose,
  className,
}: {
  org: AdminOrganization;
  onClose: () => void;
  className?: string;
}): JSX.Element {
  const root = useRef<HTMLElement>(null);

  useEffect(() => {
    root.current?.focus();
  }, []);

  return (
    <aside
      ref={root}
      tabIndex={-1}
      aria-label="Organization peek"
      className={cn("flex flex-col rounded-lg border outline-none", className)}
    >
      <div className="flex items-start justify-between gap-2 p-4 pb-3">
        <div className="min-w-0">
          <h5 className="truncate text-sm font-medium">{org.name}</h5>
          <p className="text-muted-foreground truncate text-xs">{org.slug}</p>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Close peek"
          onClick={onClose}
        >
          <XIcon />
        </Button>
      </div>

      {/* min-h-0 or the fields push the actions off the bottom. */}
      <div className="min-h-0 flex-1 overflow-auto px-4">
        <dl className="grid grid-cols-[5.5rem_1fr] items-baseline gap-x-3 gap-y-1.5">
          <Field label="Type">
            <Badge variant="outline" className={badgeTone.neutral}>
              {org.account_type}
            </Badge>
          </Field>
          <Field label="Trial ends">
            {fmtDateShort(org.free_trial_ends_at)}
          </Field>
          <Field label="Members">{org.member_count}</Field>
          <Field label="Created">{fmtDateShort(org.created_at)}</Field>
          <Field label="Org id">
            <CopyValue label="Org id" value={org.id} />
          </Field>
          <Field label="WorkOS id">
            {org.workos_id ? (
              <CopyValue label="WorkOS id" value={org.workos_id} />
            ) : (
              "-"
            )}
          </Field>
        </dl>
      </div>

      <Separator />
      {/* Empty on purpose: reserved so later actions do not move the grid. */}
      <div className="flex min-h-8 items-center gap-2 p-4" />
    </aside>
  );
}
