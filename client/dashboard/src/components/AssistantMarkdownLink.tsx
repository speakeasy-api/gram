import { Link } from "@speakeasy-api/moonshine";
import { type ComponentPropsWithoutRef, type ReactElement } from "react";

/**
 * `<a>`-shaped wrapper around Moonshine's `Link`, supplied to Elements as the
 * `linkComponent` so links inside Project Assistant replies render with the
 * dashboard's design system. Resolved entity links arrive with
 * `target="_blank"` (see {@link useAssistantLinkResolver}).
 */
export function AssistantMarkdownLink({
  href,
  children,
  ...props
}: ComponentPropsWithoutRef<"a">): ReactElement {
  return (
    <Link {...props} href={href ?? "#"}>
      {children}
    </Link>
  );
}
