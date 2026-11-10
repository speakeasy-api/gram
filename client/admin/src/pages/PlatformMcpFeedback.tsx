import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { badgeTone } from "@/lib/badgeTone";
import { errorMessage, gramAdminFetch } from "@/lib/gramAdminApi";

type PlatformMcpFeedback = {
  id: string;
  organization_id: string;
  organization_name?: string;
  organization_slug?: string;
  subject_urn: string;
  user_email?: string;
  category: string;
  rating?: number;
  success?: boolean;
  tool_name?: string;
  failure_category?: string;
  note?: string;
  delivery_state: string;
  created_at: string;
  expires_at: string;
};

const platformMcpFeedbackQuery = {
  queryKey: ["admin", "platform-mcp-feedback"],
  queryFn: () =>
    gramAdminFetch<{ feedback: PlatformMcpFeedback[] }>(
      "/admin/platformMcpFeedback.list",
    ),
};

const GOOD_CATEGORIES = new Set(["success"]);

function categoryTone(category: string): string {
  return GOOD_CATEGORIES.has(category) ? badgeTone.success : badgeTone.warning;
}

function sentAt(value: string): string {
  return new Date(value).toLocaleString("en-GB", {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export function PlatformMcpFeedbackPage(): JSX.Element {
  const feedback = useQuery(platformMcpFeedbackQuery);
  const rows = feedback.data?.feedback;

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Platform MCP feedback</h1>
        <p className="text-muted-foreground">
          Reports users sent with the send_platform_mcp_feedback tool, newest
          first. Each report is deleted 180 days after it was sent.
        </p>
      </div>
      {feedback.isPending && <p>Loading feedback…</p>}
      {feedback.isError && (
        <div role="alert">
          {errorMessage(feedback.error)}{" "}
          <Button onClick={() => void feedback.refetch()}>Retry</Button>
        </div>
      )}
      {rows && rows.length === 0 && (
        <p className="text-muted-foreground">No feedback has been sent yet.</p>
      )}
      {rows && rows.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Sent</TableHead>
              <TableHead>Organization</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Category</TableHead>
              <TableHead>Rating</TableHead>
              <TableHead>Tool</TableHead>
              <TableHead>Failure</TableHead>
              <TableHead>Note</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell className="whitespace-nowrap">
                  {sentAt(row.created_at)}
                </TableCell>
                <TableCell>
                  <Link
                    to="/organizations/$idOrSlug"
                    params={{ idOrSlug: row.organization_id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {row.organization_name ?? row.organization_id}
                  </Link>
                </TableCell>
                <TableCell>{row.user_email ?? row.subject_urn}</TableCell>
                <TableCell>
                  <Badge
                    variant="outline"
                    className={categoryTone(row.category)}
                  >
                    {row.category.replaceAll("_", " ")}
                  </Badge>
                </TableCell>
                <TableCell>{row.rating ? `${row.rating}/5` : "–"}</TableCell>
                <TableCell className="font-mono text-xs">
                  {row.tool_name ?? "–"}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {row.failure_category ?? "–"}
                </TableCell>
                <TableCell className="max-w-md whitespace-pre-wrap">
                  {row.note ?? "–"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
