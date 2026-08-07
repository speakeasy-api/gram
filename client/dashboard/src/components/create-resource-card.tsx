import { cn } from "@/lib/utils";
import { Plus } from "lucide-react";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";

type CreateResourceCardProps = {
  title: React.ReactNode;
  description: React.ReactNode;
  onClick: () => void;
  actionLabel?: string;
  className?: string;
};

export function CreateResourceCard({
  title,
  description,
  onClick,
  actionLabel = "Create",
  className,
}: CreateResourceCardProps): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn("w-full text-left hover:no-underline", className)}
    >
      <Card.Entity
        icon={
          <Plus className="text-muted-foreground group-hover:text-primary h-10 w-10 transition-colors" />
        }
        className="!border-foreground/10 hover:!border-foreground/20 border-dashed"
      >
        <Text
          variant="subheading"
          as="div"
          className="text-md text-muted-foreground group-hover:text-primary transition-colors"
        >
          {title}
        </Text>
        <Text small muted className="mb-3">
          {description}
        </Text>

        <div className="mt-auto flex items-center justify-end pt-2">
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>{actionLabel}</span>
            <Plus className="h-3.5 w-3.5" />
          </div>
        </div>
      </Card.Entity>
    </button>
  );
}
