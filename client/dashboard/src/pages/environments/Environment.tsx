import { DeleteButton } from "@/components/delete-button";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import {
  useRegisterEnvironmentTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { EnvironmentEntry } from "@gram/client/models/components";
import {
  useDeleteEnvironmentMutation,
  useListToolsets,
  useToolset,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query/index.js";
import { Stack, Table } from "@speakeasy-api/moonshine";
import { PencilIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useEnvironments } from "./Environments";
import { MoreActions } from "@/components/ui/more-actions";

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
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            {initialEntry ? "Update Environment Entry" : "New Variable"}
          </Dialog.Title>
          <Dialog.Description>
            {initialEntry
              ? "Update the environment variable value."
              : "Add a new environment variable."}
          </Dialog.Description>
        </Dialog.Header>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Type>Name</Type>
            <Input
              value={name}
              onChange={setName}
              disabled={!!initialEntry}
              onFocus={preventSelect}
            />
          </div>
          <div className="grid gap-2">
            <Type>Value</Type>
            <Input value={value} onChange={setValue} onFocus={preventSelect} />
          </div>
        </div>
        <Dialog.Footer>
          <Button variant="ghost" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!isValid}>
            {initialEntry ? "Update" : "Add"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

interface ToolsetDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (toolsetSlug: string) => void;
}

function ToolsetDialog({ open, onOpenChange, onSubmit }: ToolsetDialogProps) {
  const { data: toolsetsData } = useListToolsets();
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
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Fill for Toolset</Dialog.Title>
          <Dialog.Description>
            <p>
              Select a toolsets you would like to prefill environment variables
              for. All relevant env variables will be filled in with empty
              placeholders.
            </p>
            <br />
            <p>
              When an API has multiple optional security options, you only need
              to provide values for the security scheme relevant to you and you
              can remove the uneeded entries.
            </p>
            <br />
            <p>
              If your API has a default server URL, providing a value for a
              server URL is not required.
            </p>
          </Dialog.Description>
        </Dialog.Header>
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
        <Dialog.Footer>
          <Button variant="ghost" onClick={handleClose}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!selectedToolset}>
            Fill Variables
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
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
  const telemetry = useTelemetry();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [toolsetDialogOpen, setToolsetDialogOpen] = useState(false);
  const [editingEntry, setEditingEntry] = useState<
    EnvironmentEntry | undefined
  >(undefined);
  const [selectedToolsetSlug, setSelectedToolsetSlug] = useState<string>("");

  useRegisterEnvironmentTelemetry({
    environmentSlug: environment?.slug ?? "",
  });

  const deleteEnvironmentMutation = useDeleteEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_deleted",
      });
      environment!.refetch();
      navigate("/environments");
    },
  });

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_updated",
      });
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
    { slug: selectedToolsetSlug },
    undefined,
    { enabled: !!selectedToolsetSlug }
  );

  useEffect(() => {
    if (selectedToolset && (selectedToolset.securityVariables || selectedToolset.serverVariables)) {
      const newEntries = { ...entries };
      
      // Process security variables
      selectedToolset.securityVariables?.forEach((entry) => {
        entry.envVariables.forEach((varName) => {
          if (!entries[varName]) {
            newEntries[varName] = {
              name: varName,
              value: "",
              createdAt: new Date(),
              updatedAt: new Date(),
            };
          }
        });
      });
      
      // Process server variables
      selectedToolset.serverVariables?.forEach((entry) => {
        entry.envVariables.forEach((varName) => {
          if (!entries[varName]) {
            newEntries[varName] = {
              name: varName,
              value: "",
              createdAt: new Date(),
              updatedAt: new Date(),
            };
          }
        });
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
        slug: environment!.slug,
        updateEnvironmentRequestBody: {
          entriesToUpdate: updatedEntries,
          entriesToRemove: removedEntries.map((e) => e.name),
        },
      },
    });
  };

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
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>{environment.name}</Page.Section.Title>
          <Page.Section.CTA
            onClick={() => {
              setEditingEntry(undefined);
              setDialogOpen(true);
            }}
          >
            New Variable
          </Page.Section.CTA>
          <MoreActions
            actions={[
              {
                label: "Fill for Toolset",
                onClick: () => setToolsetDialogOpen(true),
                icon: "copy-plus",
              },
              {
                label: "Delete Environment",
                onClick: () =>
                  deleteEnvironmentMutation.mutate({
                    request: { slug: environment.slug },
                  }),
                icon: "trash",
                destructive: true,
              },
            ]}
          />
          <Page.Section.Body>
            {Object.keys(entries).length > 0 && (
              <Table
                columns={[
                  {
                    key: "name",
                    header: "Name",
                    width: "1fr",
                    render: (entry: EnvironmentEntry) => (
                      <Type variant="body" className="truncate">
                        {entry.name}
                      </Type>
                    ),
                  },
                  {
                    key: "value",
                    header: "Value",
                    width: "1fr",
                    render: (entry: EnvironmentEntry) => (
                      <Type variant="body" className="truncate">
                        {entry.value}
                      </Type>
                    ),
                  },
                  {
                    key: "actions",
                    header: "",
                    width: "100px",
                    render: (entry: EnvironmentEntry) => (
                      <div className="flex items-center gap-2">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-4 w-4 p-0"
                          onClick={() => {
                            setEditingEntry(entry);
                            setDialogOpen(true);
                          }}
                        >
                          <PencilIcon className="h-3 w-3" />
                        </Button>
                        <DeleteButton
                          tooltip="Remove Entry"
                          onClick={() => removeEntry(entry)}
                        />
                      </div>
                    ),
                  },
                ]}
                data={Object.values(entries)}
                rowKey={(row) => row.name}
              />
            )}
            {hasChanges && (
              <Stack
                direction="horizontal"
                gap={1}
                justify="end"
                className="mt-4"
              >
                <Button variant="ghost" onClick={discardChanges}>
                  Discard
                </Button>
                <Button onClick={commitUpdates}>Save</Button>
              </Stack>
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
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
