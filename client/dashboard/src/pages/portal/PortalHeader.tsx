import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import type { Portal } from "@gram/client/models/components";

interface Props {
  portal: Portal;
}

export function PortalHeader({ portal }: Props) {
  return (
    <header className="flex items-center gap-4 border-b pb-6">
      {portal.logoUrl ? (
        <img
          src={portal.logoUrl}
          alt=""
          className="h-12 w-12 rounded object-contain"
        />
      ) : null}
      <div>
        <Heading variant="h2">{portal.displayName}</Heading>
        {portal.tagline ? (
          <Type muted className="mt-1">
            {portal.tagline}
          </Type>
        ) : null}
      </div>
    </header>
  );
}
