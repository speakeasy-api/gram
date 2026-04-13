import { Button } from "@speakeasy-api/moonshine";
import { PlusIcon } from "lucide-react";

export const AddButton = ({ onClick }: { onClick?: () => void }) => {
  return (
    <Button
      variant="tertiary"
      className="text-muted-foreground hover:text-foreground"
      onClick={onClick}
    >
      <Button.LeftIcon>
        <PlusIcon className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text className="sr-only">Add</Button.Text>
    </Button>
  );
};
