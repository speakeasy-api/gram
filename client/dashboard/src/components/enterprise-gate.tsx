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
    <div className="bg-muted/20 m-8 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-24">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name={icon} className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        {title}
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
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
