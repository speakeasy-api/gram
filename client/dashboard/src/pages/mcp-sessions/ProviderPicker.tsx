import { DETAIL_HEADER_NAME } from "@/components/detail/detail-frame";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { Check, ChevronDown } from "lucide-react";
import { useState } from "react";
import { providerScopeLabel } from "./providers";

/** Beyond this many, the picker offers a search box. */
const SEARCH_THRESHOLD = 8;

/**
 * The header's name, as a picker: the current MCP Authentication Provider,
 * opening the others this project's servers can use.
 */
export function ProviderPicker({
  current,
  providers,
  onSelect,
}: {
  current: UserSessionIssuer;
  providers: UserSessionIssuer[];
  onSelect: (provider: UserSessionIssuer) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  // Nothing to switch to: the name alone.
  if (providers.length <= 1) {
    return (
      <h1 className={cn(DETAIL_HEADER_NAME, "min-w-0")}>{current.slug}</h1>
    );
  }
  const organizationWide = providers.filter((p) => p.projectId === "");
  const projectOwn = providers.filter((p) => p.projectId !== "");

  const item = (provider: UserSessionIssuer) => (
    <CommandItem
      key={provider.id}
      // cmdk matches on the value, so the slug is searchable; the id keeps
      // two providers with one slug apart.
      value={`${provider.slug} ${provider.id}`}
      onSelect={() => {
        setOpen(false);
        if (provider.id !== current.id) onSelect(provider);
      }}
    >
      <Check
        aria-hidden="true"
        className={cn(
          "size-4 shrink-0",
          provider.id === current.id ? "opacity-100" : "opacity-0",
        )}
      />
      <span className="min-w-0 flex-1 truncate">{provider.slug}</span>
    </CommandItem>
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      {/* The heading holds the button, not the reverse: a button may only
          contain phrasing content. */}
      <h1 className={cn(DETAIL_HEADER_NAME, "min-w-0")}>
        <PopoverTrigger
          aria-label={`MCP Authentication Provider: ${current.slug}. Switch provider`}
          className="hover:text-foreground/80 inline-flex max-w-full min-w-0 items-center gap-2 text-left transition-colors"
        >
          <span className="min-w-0 break-words">{current.slug}</span>
          <ChevronDown
            aria-hidden="true"
            className="text-muted-foreground size-5 shrink-0"
          />
        </PopoverTrigger>
      </h1>
      <PopoverContent
        align="start"
        className="w-[360px] max-w-[calc(100vw-2rem)] p-0"
      >
        <Command>
          {providers.length > SEARCH_THRESHOLD ? (
            <CommandInput placeholder="Find a provider" />
          ) : null}
          <CommandList>
            <CommandEmpty>No provider matches.</CommandEmpty>
            {organizationWide.length > 0 ? (
              <CommandGroup heading={providerScopeLabel({ projectId: "" })}>
                {organizationWide.map(item)}
              </CommandGroup>
            ) : null}
            {projectOwn.length > 0 ? (
              <CommandGroup heading={providerScopeLabel({ projectId: "-" })}>
                {projectOwn.map(item)}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
