import { Heading } from "@/components/ui/heading";
import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { useToolset } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router";

export function MCPHostedPage() {
  const session = useSession();
  const project = useProject();
  const { toolsetSlug } = useParams();

  const { data: toolset } = useToolset({ slug: toolsetSlug! });

  const [rawHtml, setRawHtml] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    if (!toolset?.mcpSlug) return;

    setIsLoading(true);
    setError(null);

    fetch(getServerURL() + "/mcp/" + toolset?.mcpSlug + "/page", {
      headers: {
        "Gram-Session": session.session,
        "Gram-Project": project.slug,
      },
    })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`HTTP error! status: ${res.status}`);
        }
        return res.text();
      })
      .then((html) => {
        setRawHtml(html);
        setIsLoading(false);
      })
      .catch((err) => {
        setError(err.message);
        setIsLoading(false);
      });
  }, [toolset?.mcpSlug, session.session, project.slug]);

  const iframeRef = useRef<HTMLIFrameElement>(null);

  useEffect(() => {
    if (iframeRef.current && rawHtml) {
      const doc = iframeRef.current.contentDocument;
      if (doc) {
        doc.open();
        doc.write(rawHtml);
        doc.close();
      }
    }
  }, [rawHtml]);

  if (!toolset?.mcpSlug) {
    return <div>No MCP slug found</div>;
  }

  if (error) {
    return (
      <Stack className="w-full h-full">
        <Heading variant="h2" className="mb-8">
          Hosted Page Preview
        </Heading>
        <div className="w-full h-full border-2 rounded-xl p-4 flex items-center justify-center">
          <div className="text-red-600">Error loading page: {error}</div>
        </div>
      </Stack>
    );
  }

  if (isLoading) {
    return (
      <Stack className="w-full h-full">
        <Heading variant="h2" className="mb-8">
          Hosted Page Preview
        </Heading>
        <div className="w-full h-full border-2 rounded-xl p-4 flex items-center justify-center">
          <div>Loading...</div>
        </div>
      </Stack>
    );
  }

  return (
    <Stack className="w-full h-full">
      <Heading variant="h2" className="mb-8">
        Hosted Page Preview
      </Heading>
      <div className="w-full h-full border-2 rounded-xl overflow-hidden mb-12">
        <iframe
          ref={iframeRef}
          title="MCP Hosted Preview"
          style={{ width: "100%", height: "100%", border: "none" }}
        />
      </div>
    </Stack>
  );
}
