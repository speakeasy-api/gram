import { Button } from "@/components/ui/Button";
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
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { Check, Plus, X } from "lucide-react";
import { useState, type JSX, type ReactNode } from "react";
import type { ResourceType } from "./types";

export interface ScopeGroup {
  label: string;
  resourceType: ResourceType;
  description: string;
  scopes: ScopeDefinition[];
}

/**
 * The permissions a role carries, as a list you build rather than a tree you
 * walk: pick permissions from one searchable menu, then narrow each one on its
 * own row. MCP permissions are separated from the rest because they are the
 * ones with something to narrow — a server, and inside it, particular tools.
 */
export function RolePermissionsSection({
  groups,
  selectedScopes,
  disabled,
  onToggleScope,
  renderScopeRule,
}: {
  groups: ScopeGroup[];
  selectedScopes: Set<string>;
  disabled?: boolean;
  onToggleScope: (scope: Scope) => void;
  /** Right-hand side of a row: the rule chips that narrow this permission. */
  renderScopeRule: (scope: ScopeDefinition) => ReactNode;
}): JSX.Element {
  const [tab, setTab] = useState("mcp");
  const [pickerOpen, setPickerOpen] = useState(false);

  const mcpGroups = groups.filter((group) => group.resourceType === "mcp");
  const otherGroups = groups.filter((group) => group.resourceType !== "mcp");

  const selectedIn = (list: ScopeGroup[]) =>
    list.flatMap((group) =>
      group.scopes.filter((scope) => selectedScopes.has(scope.slug)),
    );

  const mcpSelected = selectedIn(mcpGroups);
  const otherSelected = selectedIn(otherGroups);
  const activeGroups = tab === "mcp" ? mcpGroups : otherGroups;
  const activeSelected = tab === "mcp" ? mcpSelected : otherSelected;

  return (
    <div className="border-border border-t pt-4">
      <Text variant="body" className="font-medium">
        Add permissions
      </Text>
      <div className="border-border mt-3 border">
        {/* gap-0: the tab strip and the list share one bordered box, so the
            Tabs default gap left the first row sitting lower than the rest. */}
        <Tabs value={tab} onValueChange={setTab} className="gap-0">
          <div className="border-border bg-muted/30 flex items-center justify-between gap-3 border-b px-4">
            <PageTabsList>
              <PageTabsTrigger value="mcp">
                MCP access ({mcpSelected.length})
              </PageTabsTrigger>
              <PageTabsTrigger value="organization">
                Platform access ({otherSelected.length})
              </PageTabsTrigger>
            </PageTabsList>

            <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
              <PopoverTrigger asChild>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={disabled}
                  // The bar behind it is tinted, so the button keeps its own
                  // surface rather than dissolving into the header.
                  className="bg-background"
                >
                  <Button.LeftIcon>
                    <Plus className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add permissions</Button.Text>
                </Button>
              </PopoverTrigger>
              <PopoverContent
                align="end"
                className="w-[min(24rem,calc(100vw-2rem))] p-0"
              >
                <Command className="[&_[data-slot=command-input-wrapper]]:h-10 [&_[data-slot=command-input]]:h-10">
                  <CommandInput placeholder="Search permissions" />
                  <CommandList className="max-h-80">
                    <CommandEmpty>No permission matches.</CommandEmpty>
                    {activeGroups.map((group) => (
                      <CommandGroup key={group.label} heading={group.label}>
                        {group.scopes.map((scope) => (
                          <CommandItem
                            key={scope.slug}
                            value={`${scope.slug} ${scope.description}`}
                            onSelect={() => onToggleScope(scope.slug as Scope)}
                            className="items-start gap-2"
                          >
                            <div className="min-w-0 flex-1">
                              <div className="font-mono text-sm">
                                {scope.slug}
                              </div>
                              <div className="text-muted-foreground text-xs">
                                {scope.description}
                              </div>
                            </div>
                            {/* A tick marks what the role already has; a
                                checkbox implied a second, separate state. */}
                            {selectedScopes.has(scope.slug) && (
                              <>
                                {/* cmdk owns aria-selected on its items, so
                                    ownership is spoken as text instead. */}
                                <span className="sr-only">Already added</span>
                                <Check className="mt-0.5 h-4 w-4 shrink-0" />
                              </>
                            )}
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    ))}
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          </div>

          <TabsContent value={tab} forceMount>
            {activeSelected.length === 0 ? (
              <div className="px-4 py-10 text-center">
                <Text variant="body" className="font-medium">
                  {tab === "mcp"
                    ? "No MCP permissions"
                    : "No platform permissions"}
                </Text>
                <Text muted small className="mt-1">
                  {tab === "mcp"
                    ? "Add a permission to let this role reach MCP servers and their tools."
                    : "Add a permission to let this role work with projects, environments and skills."}
                </Text>
              </div>
            ) : (
              <div className="divide-border divide-y">
                {activeSelected.map((scope) => {
                  const rule = renderScopeRule(scope);
                  return (
                    <div
                      key={scope.slug}
                      className="flex items-start gap-3 px-4 py-3"
                    >
                      <div className="min-w-0 flex-1">
                        <Text
                          variant="body"
                          className="font-mono text-sm font-medium"
                        >
                          {scope.slug}
                        </Text>
                        <Text muted small>
                          {scope.description}
                        </Text>
                        {/* Only permissions that can be narrowed carry a control;
                          an empty container left the row taller than its
                          neighbours. */}
                        {rule && (
                          <div className="mt-2 flex flex-wrap items-center gap-1.5">
                            {rule}
                          </div>
                        )}
                      </div>
                      <Button
                        variant="tertiary"
                        size="sm"
                        disabled={disabled}
                        onClick={() => onToggleScope(scope.slug as Scope)}
                        aria-label={`Remove ${scope.slug}`}
                      >
                        <Button.LeftIcon>
                          <X className="h-4 w-4" />
                        </Button.LeftIcon>
                      </Button>
                    </div>
                  );
                })}
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
