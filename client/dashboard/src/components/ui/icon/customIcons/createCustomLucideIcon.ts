import { cn } from "@/lib/utils";
import { toKebabCase } from "@/components/ui/lib/utils";
import { Icon, IconNode, LucideProps } from "lucide-react";
import {
  createElement,
  forwardRef,
  type ForwardRefExoticComponent,
  type RefAttributes,
} from "react";

const createCustomLucideIcon = (
  iconName: string,
  iconNode: IconNode,
  lucideProps?: Partial<LucideProps>,
): ForwardRefExoticComponent<LucideProps & RefAttributes<SVGSVGElement>> => {
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

  Component.displayName = `${iconName}`;

  return Component;
};

export default createCustomLucideIcon;
