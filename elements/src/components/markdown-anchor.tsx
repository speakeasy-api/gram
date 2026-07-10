import { type ComponentPropsWithoutRef, memo } from "react";

import { useMarkdownLink } from "#elements/contexts/MarkdownLinkContext";
import { cn } from "#elements/lib/utils";

const ANCHOR_CLASS =
  "aui-md-a font-medium text-primary underline underline-offset-4";

/**
 * Anchor renderer shared by `<MarkdownText />` (live stream) and `<Markdown />`
 * (static viewers). Consults the host {@link useMarkdownLink}:
 *
 * - `resolveLink` rewrites internal entity references to real routes. A
 *   recognised-but-unresolvable reference (`{ href: null }`) renders as plain
 *   text, so a partial/unknown reference never becomes a dead link.
 * - `LinkComponent`, when supplied, renders every link with the host's design
 *   system (e.g. Moonshine `Link`); otherwise a plain styled `<a>` is used.
 */
export const MarkdownAnchor = memo(function MarkdownAnchor({
  className,
  href,
  children,
  // react-markdown passes the mdast `node`; never forward it to the DOM.
  node: _node,
  ...props
}: ComponentPropsWithoutRef<"a"> & { node?: unknown }) {
  const { resolveLink, LinkComponent } = useMarkdownLink();
  const resolved = href && resolveLink ? resolveLink(href) : null;

  const finalHref = resolved?.href ?? href;
  // Never render a dead anchor: an unresolvable internal reference
  // (`{ href: null }`) or an empty/whitespace href (e.g. the model emitted
  // `[name]()`) would, with `target="_blank"`, just reopen the current page in
  // a new tab. Render the text instead.
  if ((resolved && resolved.href === null) || !finalHref?.trim()) {
    return <span className={className}>{children}</span>;
  }

  const target = resolved?.target;
  const rel =
    resolved?.rel ?? (target === "_blank" ? "noopener noreferrer" : undefined);

  if (LinkComponent) {
    return (
      <LinkComponent
        className={className}
        href={finalHref}
        target={target}
        rel={rel}
        {...props}
      >
        {children}
      </LinkComponent>
    );
  }

  return (
    <a
      className={cn(ANCHOR_CLASS, className)}
      href={finalHref}
      target={target}
      rel={rel}
      {...props}
    >
      {children}
    </a>
  );
});
