import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { PromptTemplate } from "@gram/client/models/components";
import { usePrompts } from "./Prompts";

export function PromptSelectPopover({
  open,
  setOpen,
  onSelect,
  children,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  onSelect: (prompt: PromptTemplate) => void;
  children: React.ReactNode;
}) {
  const prompts = usePrompts();

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Search..." className="h-9" />
          <CommandList>
            <CommandEmpty>
              {prompts?.length === 0 ? "No prompts found." : "No items found."}
            </CommandEmpty>
            <CommandGroup>
              {prompts?.map((prompt) => (
                <CommandItem
                  key={prompt.name}
                  value={prompt.name}
                  className="cursor-pointer min-w-fit"
                  onSelect={() => {
                    onSelect(prompt);
                    setOpen(false);
                  }}
                >
                  {prompt.name}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
