"use client";

import { useDensity } from "#elements/hooks/useDensity";
import { cn } from "#elements/lib/utils";
import {
  isJsonRenderTree,
  type JsonRenderNode,
} from "#elements/lib/generative-ui";
import { AlertCircleIcon } from "lucide-react";
import { type ComponentType, type FC, type ReactNode, useMemo } from "react";

// Import all components from the generative-ui plugin ui directory
import {
  AccordionWrapper,
  AccordionItemWrapper,
  ActionButton,
  AlertWrapper,
  AvatarWrapper,
  Badge,
  ButtonWrapper,
  CardWrapper,
  CheckboxWrapper,
  DataTable,
  Grid,
  InputWrapper,
  List,
  Metric,
  Progress,
  SelectWrapper,
  Separator,
  SkeletonWrapper,
  Stack,
  TabsWrapper,
  TabContentWrapper,
  Text,
} from "#elements/plugins/generative-ui/ui";

interface GenerativeUIProps {
  /** The JSON content to render - can be a json-render tree or raw object */
  content: unknown;
  /** Additional class names */
  className?: string;
}

/**
 * Built-in components for rendering json-render trees.
 * These provide a default set of UI primitives for tool results.
 * Each entry has its own prop shape; the registry erases those generics via
 * `ComponentType` so heterogeneous components can coexist under one map.
 */
type DynamicComponentProps = Record<string, unknown> & {
  children?: ReactNode;
};

const components = {
  // Layout
  Card: CardWrapper,
  Grid,
  Stack,
  // Typography
  Text,
  // Data Display
  Metric,
  Badge,
  Table: DataTable,
  List,
  Progress,
  Avatar: AvatarWrapper,
  Skeleton: SkeletonWrapper,
  // Feedback
  Alert: AlertWrapper,
  // Structure
  Divider: Separator,
  Separator,
  // Interactive
  Accordion: AccordionWrapper,
  AccordionItem: AccordionItemWrapper,
  Tabs: TabsWrapper,
  TabContent: TabContentWrapper,
  // Actions
  Button: ButtonWrapper,
  ActionButton,
  // Form Elements
  Input: InputWrapper,
  Checkbox: CheckboxWrapper,
  Select: SelectWrapper,
} as unknown as Record<string, ComponentType<DynamicComponentProps>>;

/**
 * Recursively render a json-render tree node
 */
function renderNode(node: JsonRenderNode, key?: number): React.ReactNode {
  const Component = components[node.type];

  if (!Component) {
    // Unknown component type - render as debug info
    return (
      <div key={key} className="text-xs text-muted-foreground">
        Unknown component: {node.type}
      </div>
    );
  }

  // Recursively render children (ensure it's an array)
  const children = Array.isArray(node.children)
    ? node.children.map((child, i) => renderNode(child, i))
    : undefined;

  return (
    <Component key={key} {...(node.props ?? {})}>
      {children}
    </Component>
  );
}

/**
 * GenerativeUI component renders json-render compatible JSON as dynamic UI widgets.
 * This is used by the generativeUI plugin to render `ui` code blocks.
 */
export const GenerativeUI: FC<GenerativeUIProps> = ({ content, className }) => {
  const d = useDensity();

  // Check if content is a valid json-render tree
  const tree = useMemo(() => {
    if (isJsonRenderTree(content)) {
      return content;
    }
    return null;
  }, [content]);

  if (!tree) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 text-sm text-muted-foreground",
          d("p-md"),
          className,
        )}
      >
        <AlertCircleIcon className="size-4" />
        <span>Invalid generative UI structure</span>
      </div>
    );
  }

  return <div className={cn("w-full", className)}>{renderNode(tree)}</div>;
};

export type { GenerativeUIProps };
