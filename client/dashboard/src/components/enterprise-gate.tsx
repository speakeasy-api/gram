import { Type } from "@/components/ui/type";
import { useProductTier } from "@/hooks/useProductTier";
import { Button, Icon, IconName } from "@speakeasy-api/moonshine";
import React from "react";

interface EnterpriseGateProps {
  children: React.ReactNode;
  icon?: IconName;
  title?: string;
  description?: string;
}

export function EnterpriseGate({
  children,
  icon = "lock",
  title = "Enterprise Feature",
  description = "This feature is available on the Enterprise plan. Book a time to get started.",
}: EnterpriseGateProps) {
  const productTier = useProductTier();

  if (productTier === "enterprise") {
    return <>{children}</>;
  }

  return (
    <div className="flex flex-col items-center justify-center py-24 px-8 m-8 rounded-xl border border-dashed bg-muted/20">
      <div className="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center mb-4">
        <Icon name={icon} className="w-6 h-6 text-muted-foreground" />
      </div>
      <Type variant="subheading" className="mb-1">
        {title}
      </Type>
      <Type small muted className="text-center mb-4 max-w-md">
        {description}
      </Type>
      <Button variant="brand" asChild>
        <a
          href="https://www.speakeasy.com/book-demo"
          target="_blank"
          rel="noopener noreferrer"
        >
          Talk to our team
        </a>
      </Button>
    </div>
  );
}
