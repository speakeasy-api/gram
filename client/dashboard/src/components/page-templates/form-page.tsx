import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

/**
 * FormPage — a single create/edit form under a page header. Split out of
 * SettingsPage so a lone `<form>` (create MCP server, new prompt, policy
 * editor) stops masquerading as a stack of config sections.
 *
 * The caller owns the `<form>` and its submit; the template owns the frame,
 * scope gate, header, and the centered narrow column.
 *
 *   <FormPage scope="prompt:write" title="New prompt" description="…">
 *     <form onSubmit={onSubmit} className="flex flex-col gap-4">…</form>
 *   </FormPage>
 */
export function FormPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  title,
  description,
  stage,
  area,
  primaryAction,
  children,
  /** Max column width. Default: a comfortable single-form measure. */
  width = "narrow",
  className,
}: TemplateFrameProps &
  TemplateHeaderProps & {
    children: ReactNode;
    width?: "narrow" | "wide";
    className?: string;
  }): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
    >
      <div className={cn(width === "narrow" ? "max-w-2xl" : "max-w-4xl")}>
        <TemplateHeader
          title={title}
          description={description}
          stage={stage}
          area={area}
          primaryAction={primaryAction}
        />
        <div className={cn(className)}>{children}</div>
      </div>
    </TemplateFrame>
  );
}
