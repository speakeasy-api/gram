import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import React, { Children, isValidElement, useMemo, useState } from "react";
import { ComponentProps, ReactNode } from "react";
import {
  Group,
  Panel,
  PanelImperativeHandle,
  Separator,
} from "react-resizable-panels";

export interface ResizeHandleProps extends ComponentProps<typeof Separator> {
  children?: ReactNode;
}

const ResizeHandle = ({ children, ...props }: ResizeHandleProps) => {
  return <Separator {...props}>{children}</Separator>;
};

ResizeHandle.displayName = "ResizablePanel.ResizeHandle";

export interface ResizablePanelProps extends Omit<
  ComponentProps<typeof Group>,
  "children" | "className" | "onLayoutChange" | "orientation"
> {
  children: ReactNode;
  className?: string;
  direction?: "horizontal" | "vertical";
  onLayout?: ComponentProps<typeof Group>["onLayoutChange"];
  useDefaultHandle?: boolean;
}

const ResizablePanel = ({
  children,
  className,
  direction = "horizontal",
  useDefaultHandle = true,
  onLayout,
  resizeTargetMinimumSize = { coarse: 10, fine: 10 },
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
    <Group
      onLayoutChange={onLayout}
      orientation={direction}
      resizeTargetMinimumSize={resizeTargetMinimumSize}
      {...props}
      className={className}
    >
      {React.Children.map(validChildren, (child, index) => {
        if (!isValidElement(child)) return child;
        return (
          <>
            {child}

            {index < validChildren.length - 1 && useDefaultHandle && (
              <DefaultResizeHandle direction={direction} />
            )}
          </>
        );
      })}
    </Group>
  );
};

export interface PaneProps extends Omit<
  ComponentProps<typeof Panel>,
  "children" | "className" | "panelRef"
> {
  children: ReactNode;
  className?: string;
  panelRef?: React.Ref<PanelImperativeHandle | null>;
}

const Pane = ({ children, className, panelRef, ...props }: PaneProps) => {
  return (
    <Panel className={className} {...props} panelRef={panelRef}>
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
    <Separator
      onPointerDown={() => setIsResizing(true)}
      onPointerUp={() => setIsResizing(false)}
      onPointerCancel={() => setIsResizing(false)}
      className={cn(
        "relative border-[1.25px] border-zinc-900/50",
        isResizing && "border-foreground/10",
      )}
    >
      <div
        className={cn(
          // Centred on both axes; translating only X left the grip hanging
          // half a grip below the separator.
          "absolute top-[50%] flex translate-x-[-50%] translate-y-[-50%] items-center justify-center border bg-card text-body-muted shadow-sm shadow-zinc-400/5",
          direction === "vertical" ? "cursor-ns-resize" : "cursor-ew-resize",
          isResizing && "text-foreground",
        )}
      >
        <Icon name="grip-vertical" className="h-8 w-5" />
      </div>
    </Separator>
  );
};

const ResizablePanelWithSubcomponents = Object.assign(ResizablePanel, {
  Pane,
  ResizeHandle,
});

export { ResizablePanelWithSubcomponents as ResizablePanel };
