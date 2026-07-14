import * as React from "react";

import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for pages that configure something: org settings, server
 * settings, billing, webhooks. Stacked hairline-separated groups, each a
 * mono-labeled heading, a sentence of explanation, and its controls.
 *
 *   <SettingsLayout>
 *     <SettingsLayout.Header title="Settings" subtitle="…" />
 *     <SettingsLayout.Group
 *       label="Authentication"
 *       description="How agents prove who they are."
 *     >
 *       {fields}
 *     </SettingsLayout.Group>
 *     <SettingsLayout.DangerZone>{destructiveActions}</SettingsLayout.DangerZone>
 *   </SettingsLayout>
 */
function SettingsLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return <Layout className={className}>{children}</Layout>;
}

function SettingsLayoutBody({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Layout.Body className={cn("max-w-3xl gap-0", className)}>
      {children}
    </Layout.Body>
  );
}

/** One configuration group, separated from the next by a hairline. */
function SettingsLayoutGroup({
  label,
  description,
  actions,
  children,
  className,
}: {
  label: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cn(
        "border-neutral-softest flex flex-col gap-4 border-b py-8 first:pt-0 last:border-b-0",
        className,
      )}
    >
      <div className="flex items-start justify-between gap-6">
        <div className="flex min-w-0 flex-col gap-1">
          <Layout.Eyebrow>{label}</Layout.Eyebrow>
          {description ? (
            <Type muted small>
              {description}
            </Type>
          ) : null}
        </div>
        {actions ? <Layout.Actions>{actions}</Layout.Actions> : null}
      </div>
      {children}
    </section>
  );
}

/** Destructive actions, held apart by a destructive-toned rule. */
SettingsLayoutRoot.Header = Layout.Header;
SettingsLayoutRoot.Body = SettingsLayoutBody;
SettingsLayoutRoot.Group = SettingsLayoutGroup;
SettingsLayoutRoot.DangerZone = Layout.DangerZone;
SettingsLayoutRoot.Actions = Layout.Actions;

export { SettingsLayoutRoot as SettingsLayout };
