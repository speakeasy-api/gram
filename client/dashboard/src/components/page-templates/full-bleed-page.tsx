import type { ReactNode } from "react";
import { TemplateFrame, type TemplateFrameProps } from "./scaffold";

/**
 * FullBleedPage — the escape hatch, NOT a content template.
 *
 * The immersive canvases (Chat, Playground, Tool Builder, Assistant onboarding,
 * Elements) are two-pane / resizable-split surfaces where one pane edits and
 * the other previews or executes. Their bodies are irreducibly bespoke; only
 * the chrome is shared. This gives them the standard frame + a full-viewport,
 * zero-padding body to build inside.
 *
 * If your page fits one of the real templates (ResourceList, Detail, Tabbed,
 * Form, Settings, Overview, Workbench, Wizard), use that instead. Reach for
 * FullBleedPage only for a genuine custom app surface — and get a design review.
 *
 * For the split layout, compose with `ResizablePanel` (re-exported below).
 */
export function FullBleedPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  children,
}: TemplateFrameProps & {
  children: ReactNode;
}): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
      fullWidth
      fullHeight
      noPadding
      overflowHidden
      fullWidthBreadcrumbs
    >
      {children}
    </TemplateFrame>
  );
}

// The split-pane primitive for the escape-hatch canvases.
export { ResizablePanel } from "@/components/ui/ResizablePanel";
