import { Heading } from "@/components/ui/heading";
import { useProject } from "@/contexts/Auth";
import {
  useDeleteEnvironmentMutation,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query/index.js";
import { EnvironmentEntry } from "@gram/client/models/components";
import { useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { useEnvironments } from "./Environments";
import { Stack } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { useEffect, useState } from "react";
import { DeleteButton } from "@/components/delete-button";
import { PencilIcon } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useListToolsets, useToolset } from "@gram/client/react-query/index.js";

interface EntryDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (entry: { name: string; value: string }) => void;
  validateName?: (name: string) => boolean;
  initialEntry?: EnvironmentEntry;
}

function EntryDialog({
  open,
  onOpenChange,
  onSubmit,
  validateName,
  initialEntry,
}: EntryDialogProps) {
  const [name, setName] = useState(initialEntry?.name ?? "");
  const [value, setValue] = useState("");

  useEffect(() => {
    if (initialEntry) {
      setName(initialEntry.name);
    }
  }, [initialEntry]);

  const handleClose = () => {
    onOpenChange(false);
    setName("");
    setValue("");
  };

  const handleSubmit = () => {
    onSubmit({ name, value });
    handleClose();
  };

  const isValid = initialEntry
    ? value.length > 0
    : (validateName?.(name) ?? true) && value.length > 0;

  const preventSelect = (e: React.FocusEvent<HTMLInputElement>) => {
    e.preventDefault();
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {initialEntry ? "Update Environment Entry" : "New Variable"}
          </DialogTitle>
          <DialogDescription>
            {initialEntry
              ? "Update the environment variable value."
              : "Add a new environment variable."}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Type>Name</Type>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={!!initialEntry}
              onFocus={preventSelect}
            />
          </div>
          <div className="grid gap-2">
            <Type>Value</Type>
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              onFocus={preventSelect}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!isValid}>
            {initialEntry ? "Update" : "Add"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface ToolsetDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (toolsetSlug: string) => void;
}

function ToolsetDialog({ open, onOpenChange, onSubmit }: ToolsetDialogProps) {
  const project = useProject();
  const { data: toolsetsData } = useListToolsets({
    gramProject: project.projectSlug,
  });
  const [selectedToolset, setSelectedToolset] = useState<string>("");

  useEffect(() => {
    if (!open) {
      setSelectedToolset("");
    }
  }, [open]);

  const handleClose = () => {
    onOpenChange(false);
    setSelectedToolset("");
  };

  const handleSubmit = () => {
    onSubmit(selectedToolset);
    handleClose();
  };

  const options =
    toolsetsData?.toolsets.map((toolset) => ({
      label: toolset.name,
      value: toolset.slug,
    })) || [];

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Fill for Toolset</DialogTitle>
          <DialogDescription>
            Select a list toolsets you would like to prefill environment
            variables for. All possible env variables will be filled in as empty
            values, set any relevant variables and remove uneeded ones.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Type>Toolset</Type>
            <select
              value={selectedToolset}
              onChange={(e) => setSelectedToolset(e.target.value)}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="">Select a toolset</option>
              {options.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!selectedToolset}>
            Fill Variables
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function useEnvironment(slug?: string) {
  let { environmentSlug } = useParams();
  if (slug) environmentSlug = slug;

  const environments = useEnvironments();

  const environment = environments.find(
    (environment) => environment.slug === environmentSlug
  );

  return environment
    ? Object.assign(environment, { refetch: environments.refetch })
    : null;
}

export default function EnvironmentPage() {
  const environment = useEnvironment();
  const navigate = useNavigate();
  const project = useProject();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [toolsetDialogOpen, setToolsetDialogOpen] = useState(false);
  const [editingEntry, setEditingEntry] = useState<
    EnvironmentEntry | undefined
  >(undefined);
  const [selectedToolsetSlug, setSelectedToolsetSlug] = useState<string>("");

  const deleteEnvironmentMutation = useDeleteEnvironmentMutation({
    onSuccess: () => {
      environment!.refetch();
      navigate("/environments");
    },
  });

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      environment!.refetch();
    },
  });

  const entriesFromArray = (entries: EnvironmentEntry[]) => {
    return entries.reduce((acc, entry) => {
      acc[entry.name] = entry;
      return acc;
    }, {} as Record<string, EnvironmentEntry>);
  };

  const [entries, setEntries] = useState<Record<string, EnvironmentEntry>>(
    entriesFromArray(environment?.entries ?? [])
  );

  useEffect(() => {
    if (environment) {
      setEntries(entriesFromArray(environment.entries));
    }
  }, [environment]);

  const { data: selectedToolset } = useToolset(
    {
      gramProject: project.projectSlug,
      slug: selectedToolsetSlug,
    },
    {
      enabled: !!selectedToolsetSlug,
    }
  );

  useEffect(() => {
    if (selectedToolset && selectedToolset.relevantEnvironmentVariables) {
      const newEntries = { ...entries };
      selectedToolset.relevantEnvironmentVariables.forEach((varName) => {
        if (!entries[varName]) {
          newEntries[varName] = {
            name: varName,
            value: "",
            createdAt: new Date(),
            updatedAt: new Date(),
          };
        }
      });
      setEntries(newEntries);
      setSelectedToolsetSlug("");
    }
  }, [selectedToolset, entries]);

  if (!environment) {
    return <div>Environment not found</div>;
  }

  const updateEntry = (entry: EnvironmentEntry) => {
    setEntries({ ...entries, [entry.name]: entry });
  };

  const removeEntry = (entry: EnvironmentEntry) => {
    const newEntries = { ...entries };
    delete newEntries[entry.name];
    setEntries(newEntries);
  };

  const computeChanges = () => {
    const updatedEntries = Object.values(entries).filter(
      (entry) =>
        !environment.entries.some(
          (e) => e.name === entry.name && e.value === entry.value
        )
    );
    const removedEntries = environment.entries.filter((e) => !entries[e.name]);
    return {
      updatedEntries,
      removedEntries,
    };
  };

  const commitUpdates = () => {
    const { updatedEntries, removedEntries } = computeChanges();

    updateEnvironmentMutation.mutate({
      request: {
        gramProject: project.projectSlug,
        slug: environment!.slug,
        updateEnvironmentRequestBody: {
          entriesToUpdate: updatedEntries,
          entriesToRemove: removedEntries.map((e) => e.name),
        },
      },
    });
  };

  const deleteButton = (
    <DeleteButton
      tooltip="Delete Environment"
      onClick={() => {
        if (
          confirm(
            "Are you sure you want to delete this environment? This action cannot be undone."
          )
        ) {
          deleteEnvironmentMutation.mutate({
            request: {
              gramProject: project.projectSlug,
              slug: environment.slug,
            },
          });
        }
      }}
    />
  );

  const validateEntryName = (name: string) => {
    return (
      name.length > 0 &&
      !environment?.entries.some((entry) => entry.name === name) &&
      /^[-_.a-zA-Z][-_.a-zA-Z0-9]*$/.test(name)
    );
  };

  const discardChanges = () => {
    setEntries(entriesFromArray(environment.entries));
  };

  const hasChanges =
    computeChanges().updatedEntries.length > 0 ||
    computeChanges().removedEntries.length > 0;

  const handleEntrySubmit = ({
    name,
    value,
  }: {
    name: string;
    value: string;
  }) => {
    const entry = editingEntry
      ? {
          ...editingEntry,
          value,
        }
      : {
          name,
          value,
          createdAt: new Date(),
          updatedAt: new Date(),
        };

    updateEntry(entry);
    setEditingEntry(undefined);
  };

  const handleToolsetSubmit = (toolsetSlug: string) => {
    setSelectedToolsetSlug(toolsetSlug);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{deleteButton}</Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        <Stack direction="horizontal" gap={6}>
          <Heading variant="h2">{environment.name}</Heading>
          <Stack direction="horizontal" gap={1}>
            {hasChanges ? (
              <>
                <Button onClick={commitUpdates}>Save</Button>
                <Button variant="ghost" onClick={discardChanges}>
                  Discard
                </Button>
              </>
            ) : (
              <>
                <Button
                  onClick={() => {
                    setEditingEntry(undefined);
                    setDialogOpen(true);
                  }}
                >
                  New Variable
                </Button>
                <Button
                  variant="outline"
                  onClick={() => {
                    setToolsetDialogOpen(true);
                  }}
                >
                  Fill for Toolset
                </Button>
              </>
            )}
          </Stack>
        </Stack>

        {Object.keys(entries).length > 0 && (
          <div className="mt-6 rounded-md border">
            <div className="grid grid-cols-2 bg-muted px-4 py-2">
              <Type className="font-medium">Name</Type>
              <Type className="font-medium">Value</Type>
            </div>
            <div className="divide-y">
              {Object.values(entries).map((entry) => (
                <div key={entry.name} className="px-4">
                  <EntryItem
                    entry={entry}
                    onEdit={() => {
                      setEditingEntry(entry);
                      setDialogOpen(true);
                    }}
                    removeEntry={removeEntry}
                  />
                </div>
              ))}
            </div>
          </div>
        )}

        <EntryDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          onSubmit={handleEntrySubmit}
          validateName={validateEntryName}
          initialEntry={editingEntry}
        />

        <ToolsetDialog
          open={toolsetDialogOpen}
          onOpenChange={setToolsetDialogOpen}
          onSubmit={handleToolsetSubmit}
        />
      </Page.Body>
    </Page>
  );
}

function EntryItem({
  entry,
  onEdit,
  removeEntry,
  isNew,
}: {
  entry: EnvironmentEntry;
  onEdit: () => void;
  removeEntry?: (entry: EnvironmentEntry) => void;
  isNew?: boolean;
}) {
  return (
    <div className="grid grid-cols-2 items-center py-3 group/entry">
      <div className="pr-4">
        <Type className={cn("truncate", isNew && "text-muted-foreground")}>
          {entry.name}
        </Type>
      </div>
      <div className="flex items-center gap-2 pr-4">
        <Type
          className={cn("truncate flex-1", isNew && "text-muted-foreground")}
        >
          {entry.value}
        </Type>
        <Button
          variant="ghost"
          size="icon"
          className="h-4 w-4 p-0 opacity-0 group-hover/entry:opacity-100"
          onClick={onEdit}
        >
          <PencilIcon className="h-3 w-3" />
        </Button>
        {removeEntry && (
          <DeleteButton
            tooltip="Remove Entry"
            onClick={() => removeEntry(entry)}
            className="opacity-0 group-hover/entry:opacity-100"
          />
        )}
      </div>
    </div>
  );
}
