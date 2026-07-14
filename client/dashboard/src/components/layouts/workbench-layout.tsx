import * as React from "react";

import { ResizablePanel } from "@/components/ui/resizable-panel";
import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for build-and-preview surfaces: a config side and a live
 * preview side, split by a draggable handle, filling the viewport. Used by
 * the playground, the tool builder, the assistant editor, the Elements
 * config, and the SDK generator.
 *
 * It expects to sit in a full-height, no-padding body:
 *   <Page.Body fullWidth fullHeight noPadding>
 *     <WorkbenchLayout>
 *       <WorkbenchLayout.Header eyebrow="Playground" title="Untitled" actions={…} />
 *       <WorkbenchLayout.Body
 *         config={<ConfigPanel />}
 *         preview={<PreviewPanel />}
 *       />
 *     </WorkbenchLayout>
 *   </Page.Body>
 */
function WorkbenchLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("flex h-full min-h-0 flex-col", className)}>
      {children}
    </div>
  );
}

/**
 * The header band. Slimmer than the other layouts' — a workbench earns its
 * height for the panes — but the same eyebrow / title / actions contract.
 */
function WorkbenchLayoutHeader({
  className,
  ...props
}: React.ComponentProps<typeof Layout.Header>): JSX.Element {
  return (
    <div className="shrink-0 px-6 pt-6">
      <Layout.Header className={cn("pb-4", className)} {...props} />
    </div>
  );
}

/**
 * The two-pane body. `config` and `preview` are passed as nodes rather than
 * compound children because `ResizablePanel` detects its panes by component
 * type — wrapping them would hide them from that check.
 */
function WorkbenchLayoutBody({
  config,
  preview,
  direction = "horizontal",
  defaultPreviewSize = 40,
  minConfigSize = 30,
  minPreviewSize = 24,
  className,
}: {
  config: React.ReactNode;
  preview: React.ReactNode;
  direction?: "horizontal" | "vertical";
  defaultPreviewSize?: number;
  minConfigSize?: number;
  minPreviewSize?: number;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("min-h-0 flex-1", className)}>
      <ResizablePanel
        direction={direction}
        className={cn(
          "h-full",
          "[&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:hover:bg-primary",
        )}
      >
        <ResizablePanel.Pane minSize={minConfigSize}>
          <div className="h-full min-h-0 overflow-y-auto">{config}</div>
        </ResizablePanel.Pane>
        <ResizablePanel.Pane
          minSize={minPreviewSize}
          defaultSize={defaultPreviewSize}
        >
          <div className="bg-muted/20 h-full min-h-0 overflow-y-auto">
            {preview}
          </div>
        </ResizablePanel.Pane>
      </ResizablePanel>
    </div>
  );
}

WorkbenchLayoutRoot.Header = WorkbenchLayoutHeader;
WorkbenchLayoutRoot.Body = WorkbenchLayoutBody;
WorkbenchLayoutRoot.Actions = Layout.Actions;

export { WorkbenchLayoutRoot as WorkbenchLayout };
