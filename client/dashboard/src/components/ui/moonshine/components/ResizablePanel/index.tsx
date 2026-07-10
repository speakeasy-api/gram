import { Icon } from "@/components/ui/moonshine/components/Icon";
import { cn } from "@/components/ui/moonshine/lib/utils";
import React, { Children, isValidElement, useMemo, useState } from "react";
import { ComponentProps, ReactNode } from "react";
import {
  ImperativePanelHandle,
  Panel,
  PanelGroup,
  PanelResizeHandle,
} from "react-resizable-panels";

interface ResizeHandleProps extends ComponentProps<typeof PanelResizeHandle> {
  children?: ReactNode;
}

const ResizeHandle = ({ children, ...props }: ResizeHandleProps) => {
  return <PanelResizeHandle {...props}>{children}</PanelResizeHandle>;
};

ResizeHandle.displayName = "ResizablePanel.ResizeHandle";

interface ResizablePanelProps extends ComponentProps<typeof PanelGroup> {
  children: ReactNode;
  className?: string;
  useDefaultHandle?: boolean;
}

const ResizablePanel = ({
  children,
  className,
  useDefaultHandle = true,
  onLayout,
  ...props
}: ResizablePanelProps) => {
  const validChildren = useMemo(
    () =>
      Children.toArray(children).filter((child) => {
        if (!isValidElement(child)) return false;
        const type = child.type as { displayName?: string };
        return (
          type.displayName === "ResizablePanel.Pane" ||
          type.displayName === "ResizablePanel.ResizeHandle"
        );
      }),
    [children],
  );

  return (
    <PanelGroup onLayout={onLayout} {...props} className={className}>
      {React.Children.map(validChildren, (child, index) => {
        if (!isValidElement(child)) return child;
        return (
          <>
            {child}

            {index < validChildren.length - 1 && useDefaultHandle && (
              <DefaultResizeHandle direction={props.direction} />
            )}
          </>
        );
      })}
    </PanelGroup>
  );
};

interface PaneProps extends ComponentProps<typeof Panel> {
  children: ReactNode;
  className?: string;
  panelRef?: React.LegacyRef<ImperativePanelHandle>;
}

const Pane = ({ children, className, panelRef, ...props }: PaneProps) => {
  return (
    <Panel className={className} {...props} ref={panelRef}>
      {children}
    </Panel>
  );
};

Pane.displayName = "ResizablePanel.Pane";

const DefaultResizeHandle = ({
  direction,
}: {
  direction: "horizontal" | "vertical";
}) => {
  const [isResizing, setIsResizing] = useState(false);
  return (
    <PanelResizeHandle
      onDragging={(dragging) => setIsResizing(dragging)}
      hitAreaMargins={{
        coarse: 10,
        fine: 10,
      }}
      className={cn(
        "relative border-[1.25px] border-zinc-900/50",
        isResizing && "border-foreground/10",
      )}
    >
      <div
        className={cn(
          "absolute top-[50%] flex translate-x-[-50%] items-center justify-center rounded-md border bg-card text-body-muted",
          direction === "vertical" ? "cursor-ns-resize" : "cursor-ew-resize",
          isResizing && "text-foreground",
        )}
      >
        <Icon name="grip-vertical" className="h-8 w-5" />
      </div>
    </PanelResizeHandle>
  );
};

const ResizablePanelWithSubcomponents = Object.assign(ResizablePanel, {
  Pane,
  ResizeHandle,
});

export { ResizablePanelWithSubcomponents as ResizablePanel };
