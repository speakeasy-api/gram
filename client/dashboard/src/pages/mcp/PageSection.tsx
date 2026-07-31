import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import React from "react";

export function PageSection({
  heading,
  description,
  featureType,
  action,
  headingExtra,
  children,
  className,
}: {
  heading: string;
  description: string;
  fullWidth?: boolean;
  featureType?: "experimental" | "beta";
  action?: React.ReactNode;
  headingExtra?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  return (
    <Stack gap={2} className={cn("mb-8", className)}>
      <div className="flex items-center justify-between">
        <Heading variant="h3" className="flex items-center">
          {heading}
          {featureType && (
            <Badge variant="warning" className="ml-2">
              {featureType}
            </Badge>
          )}
          {headingExtra}
        </Heading>
        {action}
      </div>
      <Text muted small className="max-w-2xl">
        {description}
      </Text>
      {children}
    </Stack>
  );
}
