import { Text } from "@/components/ui/Text";
import * as React from "react";

export function DetailSidebarInfoLabel({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <Text
      variant="small"
      muted
      className="font-mono text-xs tracking-wide uppercase"
    >
      {children}
    </Text>
  );
}

export type DetailSidebarNavItem = {
  key: string;
  title: string;
  titleNode?: React.ReactNode;
  Icon: React.ComponentType<{ className?: string }>;
  href: string;
  active: boolean;
};
