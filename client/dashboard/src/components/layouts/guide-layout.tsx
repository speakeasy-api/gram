import * as React from "react";

import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for instructional and step-through pages: onboarding uploads,
 * getting-started guides, install references. A header band over a narrow,
 * centered reading column, with an optional sticky footer for back/next
 * actions.
 *
 *   <GuideLayout>
 *     <GuideLayout.Header eyebrow="Setup" title="Upload your OpenAPI document" />
 *     <GuideLayout.Body>{steps}</GuideLayout.Body>
 *     <GuideLayout.Footer>
 *       <Button variant="secondary">Back</Button>
 *       <Button>Continue</Button>
 *     </GuideLayout.Footer>
 *   </GuideLayout>
 */
function GuideLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <Layout className={cn("mx-auto w-full max-w-3xl", className)}>
      {children}
    </Layout>
  );
}

/** The reading column: steps, prose, code blocks, forms. */
function GuideLayoutBody({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <Layout.Body className={cn("gap-8", className)}>{children}</Layout.Body>
  );
}

/** A titled step block within the guide. */
function GuideLayoutStep({
  index,
  title,
  children,
  className,
}: {
  index?: number;
  title?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <Layout.Section
      title={title}
      annotation={index != null ? `Step ${index}` : undefined}
      className={className}
    >
      {children}
    </Layout.Section>
  );
}

/** Right-aligned back/next controls, closed off by a hairline rule above. */
function GuideLayoutFooter({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "border-neutral-softest mt-8 flex items-center justify-end gap-2 border-t pt-6",
        className,
      )}
    >
      {children}
    </div>
  );
}

GuideLayoutRoot.Header = Layout.Header;
GuideLayoutRoot.Body = GuideLayoutBody;
GuideLayoutRoot.Step = GuideLayoutStep;
GuideLayoutRoot.Footer = GuideLayoutFooter;
GuideLayoutRoot.Actions = Layout.Actions;

export { GuideLayoutRoot as GuideLayout };
