import { Heading } from "@/components/ui/heading";
import { useProject } from "@/contexts/Auth";
import {
  useDeleteEnvironmentMutation,
  useUpdateEnvironmentMutation,
} from "@gram/sdk/react-query";
import { Environment, EnvironmentEntry } from "@gram/sdk/models/components";
import { useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { useEnvironments } from "./Environments";
import { Stack } from "@speakeasy-api/moonshine";
import { EditableText } from "@/components/ui/editable-text";
import { Type } from "@/components/ui/type";
import { useEffect, useState } from "react";
import { DeleteButton } from "@/components/delete-button";
import { Separator } from "@/components/ui/separator";

export function useEnvironment() {
  const { environmentSlug } = useParams();
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

  const deleteEnvironmentMutation = useDeleteEnvironmentMutation({
    onSuccess: () => {
      environment!.refetch();
      navigate("/environments");
    },
  });

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
              slug: environment!.slug,
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

  console.log(computeChanges());

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{environment && deleteButton}</Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        <Stack direction="horizontal" gap={6}>
          <Heading variant="h2">{environment.name}</Heading>
          {hasChanges && (
            <Stack direction="horizontal" gap={1}>
              <Button onClick={commitUpdates}>Save</Button>
              <Button variant="ghost" onClick={discardChanges}>
                Discard
              </Button>
            </Stack>
          )}
        </Stack>
        {Object.values(entries).map((entry) => (
          <>
            <EntryItem
              key={entry.name}
              entry={entry}
              updateEntry={updateEntry}
              removeEntry={removeEntry}
              validateEntryName={validateEntryName}
            />
            <Separator
              key={entry.name + "separator"}
              className="max-w-[400px]"
            />
          </>
        ))}
        <EntryItem
          entry={{
            name: "NEW_ENTRY",
            value: "NEW_VALUE",
            createdAt: new Date(),
            updatedAt: new Date(),
          }}
          updateEntry={updateEntry}
          validateEntryName={validateEntryName}
          isNew
        />
      </Page.Body>
    </Page>
  );
}

function EntryItem({
  entry,
  updateEntry,
  validateEntryName,
  removeEntry,
  isNew,
}: {
  entry: EnvironmentEntry;
  updateEntry: (entry: EnvironmentEntry) => void;
  validateEntryName: (name: string) => boolean;
  removeEntry?: (entry: EnvironmentEntry) => void;
  isNew?: boolean;
}) {
  return (
    <Stack direction="horizontal" gap={2} className="group/entry h-[25px]">
      <div className="w-[200px]">
        <EditableText
          value={entry.name}
          onSubmit={(newValue) => updateEntry({ ...entry, name: newValue })}
          renderDisplay={(value) => (
            <Type className={isNew ? "text-muted-foreground" : ""}>
              {value}
            </Type>
          )}
          inputClassName="text-base mb-2 px-1 border w-full"
          validate={validateEntryName}
        />
      </div>
      <EditableText
        value={entry.value}
        onSubmit={(newValue) => updateEntry({ ...entry, value: newValue })}
        renderDisplay={(value) => (
          <Type className={isNew ? "text-muted-foreground" : ""}>{value}</Type>
        )}
        inputClassName="text-base mb-2 px-1 border w-full"
      />
      {removeEntry && (
        <DeleteButton
          tooltip="Remove Entry"
          onClick={() => removeEntry(entry)}
          className="opacity-0 group-hover/entry:opacity-100"
        />
      )}
    </Stack>
  );
}
