import { cn } from "@/lib/utils";
import { Card } from "./ui/card";
import { Heading } from "./ui/heading";

export function CreateThingCard({
  onClick,
  className,
  children,
}: {
  onClick?: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <Card
      className={cn(
        "border-dashed border-2 hover:border-muted-foreground/50 bg-transparent cursor-pointer min-h-36 trans group shadow-none",
        className
      )}
      onClick={onClick}
    >
      <Card.Content className="flex items-center justify-center h-full">
        <Heading
          variant="h5"
          className="text-muted-foreground/40 group-hover:text-muted-foreground trans"
        >
          {children}
        </Heading>
      </Card.Content>
    </Card>
  );
}
