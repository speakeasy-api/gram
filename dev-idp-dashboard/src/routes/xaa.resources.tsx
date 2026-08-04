import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Cell,
  DeleteButton,
  Row,
  Section,
  Table,
} from "@/components/xaa/Section";
import {
  useCreateXaaResource,
  useDeleteXaaResource,
  useXaaResources,
} from "@/hooks/use-xaa";

export const Route = createFileRoute("/xaa/resources")({
  component: ResourcesPage,
});

function ResourcesPage() {
  const { data, isLoading } = useXaaResources();
  const create = useCreateXaaResource();
  const remove = useDeleteXaaResource();

  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [identifier, setIdentifier] = useState("");

  const submit = () => {
    if (!slug.trim() || !identifier.trim()) return;
    create.mutate(
      {
        slug: slug.trim(),
        name: name.trim() || undefined,
        resource_identifier: identifier.trim(),
      },
      {
        onSuccess: () => {
          setSlug("");
          setName("");
          setIdentifier("");
        },
      },
    );
  };

  return (
    <Section
      title="Resources"
      description={
        <>
          Each resource is one authorization server, reachable the moment it
          exists. <strong>Issuer</strong> is what an ID-JAG must carry in{" "}
          <code className="text-xs">aud</code>;{" "}
          <strong>resource identifier</strong> is the MCP server behind it and
          lands in the <code className="text-xs">resource</code> claim. They are
          different URLs — swapping them is the usual way to get this flow
          wrong.
        </>
      }
    >
      <div className="flex flex-wrap items-end gap-3 rounded-lg border border-border p-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="res-slug">Slug</Label>
          <Input
            id="res-slug"
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            placeholder="chat"
            className="w-40"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="res-name">Name</Label>
          <Input
            id="res-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="defaults to the slug"
            className="w-44"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="res-identifier">Resource identifier</Label>
          <Input
            id="res-identifier"
            value={identifier}
            onChange={(e) => setIdentifier(e.target.value)}
            placeholder="https://mcp.chat.example/"
            className="w-64"
          />
        </div>
        <Button
          onClick={submit}
          disabled={!slug.trim() || !identifier.trim() || create.isPending}
        >
          Register
        </Button>
      </div>

      {create.error ? (
        <p className="text-sm text-destructive">{String(create.error)}</p>
      ) : null}

      <Table
        headers={["Slug", "Name", "Issuer (aud)", "Resource (resource)", ""]}
        isEmpty={(data?.items ?? []).length === 0}
        empty={isLoading ? "Loading…" : "No resources registered."}
      >
        {(data?.items ?? []).map((r) => (
          <Row key={r.id}>
            <Cell mono>{r.slug}</Cell>
            <Cell>{r.name}</Cell>
            <Cell mono>{r.issuer}</Cell>
            <Cell mono>{r.resource_identifier}</Cell>
            <Cell className="text-right">
              <DeleteButton
                onClick={() => remove.mutate({ id: r.id })}
                disabled={remove.isPending}
              />
            </Cell>
          </Row>
        ))}
      </Table>
    </Section>
  );
}
