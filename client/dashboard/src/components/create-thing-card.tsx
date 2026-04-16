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
        "hover:border-muted-foreground/50 trans group min-h-36 cursor-pointer items-center justify-center border-2 border-dashed bg-transparent shadow-none",
        className,
      )}
      onClick={onClick}
    >
      <Card.Content>
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
