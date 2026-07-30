import { cn, toKebabCase } from "@/components/ui/lib/utils";
import { Icon, IconNode, LucideProps } from "lucide-react";
import * as React from "react";
import { createElement, forwardRef } from "react";

const createCustomLucideIcon = (
  iconName: string,
  iconNode: IconNode,
  lucideProps?: Partial<LucideProps>,
): React.ForwardRefExoticComponent<
  LucideProps & React.RefAttributes<SVGSVGElement>
> => {
  const Component = forwardRef<SVGSVGElement, LucideProps>(
    ({ className, ...props }, ref) =>
      createElement(Icon, {
        ref,
        iconNode,
        className: cn(`lucide-${toKebabCase(iconName)}`, className),
        ...lucideProps,
        ...props,
      }),
  );

  Component.displayName = iconName;

  return Component;
};

export default createCustomLucideIcon;
