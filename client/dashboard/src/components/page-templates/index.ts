/**
 * Page templates — the paint-by-numbers layer. Pick the template that matches
 * your page's shape and fill in the data; the frame, scope gate, single header,
 * and loading / empty / error branches come for free. See the design-system
 * consolidation plan for how all 93 routes map onto this set.
 *
 * In-app templates:
 *  - ResourceListPage — a collection: toolbar + table/grid + empty state
 *  - DetailPage       — one entity: hero + routed/scroll sections
 *  - TabbedPage       — non-entity tab areas (tabs are different resources)
 *  - FormPage         — a single create/edit form
 *  - SettingsPage     — stacked titled config sections (+ content variant)
 *  - OverviewPage     — a dashboard: stat row + summary cards
 *  - WorkbenchPage    — a fullbleed analytics/observe surface
 *  - WizardPage       — a multi-step flow (stepper rail + step body)
 *
 * Standalone shell + escape hatch:
 *  - CenteredPage     — full-viewport auth / standalone pages
 *  - FullBleedPage    — the escape hatch for bespoke app canvases
 */
export { ResourceListPage } from "./resource-list-page";
export { DetailPage, type DetailSection } from "./detail-page";
export { TabbedPage, type PageTab } from "./tabbed-page";
export { FormPage } from "./form-page";
export {
  SettingsPage,
  SettingsSection,
  DangerSettingsSection,
  FooterSaveButton,
} from "./settings-page";
export { OverviewPage } from "./overview-page";
export { WorkbenchPage } from "./workbench-page";
export { WizardPage, type WizardStep } from "./wizard-page";
export { CenteredPage, AuthShell } from "./centered-page";
export { FullBleedPage, ResizablePanel } from "./full-bleed-page";

// Shared header/frame types for callers that want to build on the scaffold.
export type { TemplateFrameProps, TemplateHeaderProps } from "./scaffold";
