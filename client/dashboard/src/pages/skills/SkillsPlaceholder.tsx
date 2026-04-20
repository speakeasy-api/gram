import { Icon } from "@speakeasy-api/moonshine";

import { Type } from "@/components/ui/type";

export function SkillsPlaceholder({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="p-8">
      <div className="mx-auto max-w-4xl space-y-3">
        <div>
          <Type variant="subheading">{title}</Type>
          <Type small muted className="mt-1 block max-w-2xl">
            {description}
          </Type>
        </div>

        <div className="bg-muted/20 flex min-h-[360px] flex-col items-center justify-center rounded-xl border border-dashed px-8 py-24 text-center">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Icon name="sparkles" className="text-muted-foreground h-6 w-6" />
          </div>
          <Type variant="subheading" className="mb-1">
            Top-level IA locked
          </Type>
          <Type small muted className="max-w-md">
            This subpage is scaffolded with a stable URL, breadcrumb, and tab
            slot. We can fill in the actual UI next without revisiting the route
            model.
          </Type>
        </div>
      </div>
    </div>
  );
}
