import { Stack } from "@/components/ui/Stack";
import type { ReactNode } from "react";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

/**
 * SettingsPage — a vertical stack of titled config sections under a page
 * header (project settings, org identity, logging, webhooks). Compose the body
 * from `SettingsSection` blocks (re-exported below so callers don't reach into
 * the mcp path). A `variant="content"` drops the section chrome for
 * prose/tool pages (SDK, onboarding docs).
 *
 *   <SettingsPage scope="project:write" title="Project settings" description="…">
 *     <SettingsSection>
 *       <SettingsSection.Header>
 *         <SettingsSection.Title>Model provider keys</SettingsSection.Title>
 *       </SettingsSection.Header>
 *       …
 *     </SettingsSection>
 *   </SettingsPage>
 */
export function SettingsPage({
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
  variant = "sections",
}: TemplateFrameProps &
  TemplateHeaderProps & {
    children: ReactNode;
    /** "sections" (default) or "content" (single narrow prose column). */
    variant?: "sections" | "content";
  }): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
    >
      <TemplateHeader
        title={title}
        description={description}
        stage={stage}
        area={area}
        primaryAction={primaryAction}
      />
      {variant === "content" ? (
        <div className="max-w-3xl">{children}</div>
      ) : (
        <Stack gap={8}>{children}</Stack>
      )}
    </TemplateFrame>
  );
}

// Promote the settings section primitives out of the mcp path so any settings
// page can compose them without an mcp-scoped import.
export {
  SettingsSection,
  DangerSettingsSection,
  FooterSaveButton,
} from "@/components/detail/settings-section";
