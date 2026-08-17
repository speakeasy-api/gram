import { CheckIcon, CopyIcon, XIcon } from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type JSX,
  type ReactNode,
  type RefObject,
} from "react";

import { Trial } from "@/components/Trial";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { cn, fmtDateShort } from "@/lib/utils";

import { OrganizationActions } from "./OrganizationActions";

const COPY_CONFIRM_MS = 1500;

// One panel is on the page at a time, so a constant is enough and the row's
// trigger can point `aria-controls` at it without threading an id through.
export const PEEK_PANEL_ID = "organization-peek-panel";

function noop(): void {}

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
  // The value copied, not a flag. Peek swaps the record under a mounted panel,
  // and a flag would stand as a confirmation against an id never copied,
  // including where the write resolves after the swap.
  const [copiedValue, setCopiedValue] = useState<string>();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const copied = copiedValue === value;

  useEffect(() => () => clearTimeout(timer.current), []);

  return (
    <span className="flex items-center gap-1">
      <span className="truncate font-mono text-xs">{value}</span>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={copied ? `${label} copied` : `Copy ${label}`}
        onClick={() => {
          // Undefined outside a secure context, where the call would throw.
          if (!navigator.clipboard?.writeText) return;
          // A check over a failed write sends the operator off with the wrong id.
          void navigator.clipboard.writeText(value).then(() => {
            setCopiedValue(value);
            clearTimeout(timer.current);
            timer.current = setTimeout(
              () => setCopiedValue(undefined),
              COPY_CONFIRM_MS,
            );
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
  ref,
}: {
  org: AdminOrganization;
  onClose: () => void;
  className?: string;
  // The caller's, where it has one. The list handles keys above both the table
  // and this panel, and it has to tell the panel apart from the controls the
  // panel contains.
  ref?: RefObject<HTMLElement | null>;
}): JSX.Element {
  const own = useRef<HTMLElement>(null);
  const root = ref ?? own;

  useEffect(() => {
    root.current?.focus();
  }, [root]);

  return (
    <aside
      ref={root}
      id={PEEK_PANEL_ID}
      // In the tab order, not just focusable. This node is the one place in the
      // subtree where the arrow keys walk the peek from record to record, and
      // at -1 the mount was the only way focus ever reached it: Tab moves to
      // Close, and Shift+Tab back skips a tabindex="-1" node, so record
      // navigation was gone for the rest of the panel's life.
      //
      // The ring comes with it. Nothing else on screen moves when focus lands
      // here, so an invisible ring on the one node with its own keys is the
      // worst of both.
      tabIndex={0}
      aria-label="Organization peek"
      className={cn("flex flex-col rounded-lg border", className)}
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
          <Field label="Trial">
            <Trial org={org} />
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
      {/* Plain buttons, not a menu: this footer sits inside the subtree the
          list watches for Escape and the arrow keys, and a menu would answer
          Escape before the panel it is drawn in. */}
      <div className="flex min-h-8 items-center gap-2 p-4">
        <OrganizationActions org={org} layout="footer" />
      </div>
    </aside>
  );
}
