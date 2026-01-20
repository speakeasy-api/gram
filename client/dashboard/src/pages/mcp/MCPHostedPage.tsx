import { Heading } from "@/components/ui/heading";
import { useProject, useSession } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { Toolset } from "@gram/client/models/components";
import { useToolset } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router";
import { useMcpUrl } from "@/hooks/useToolsetUrl";

export function MCPHostedPage() {
  const { toolsetSlug } = useParams();
  const { data: toolset } = useToolset({ slug: toolsetSlug ?? "" }, undefined, {
    enabled: !!toolsetSlug,
  });

  return (
    <Stack className="w-full h-full">
      <Heading variant="h2" className="mb-8">
        Hosted Page Preview
      </Heading>
      <MCPPagePreview
        toolset={toolset}
        height={600}
        className="border-2 rounded-xl mb-12 max-w-[1200px]"
      />
    </Stack>
  );
}

export function MCPPagePreview({
  toolset,
  height,
  className,
}: {
  toolset: Toolset | undefined;
  height: number;
  className?: string;
}) {
  const session = useSession();
  const project = useProject();
  const { installPageUrl } = useMcpUrl(toolset);

  const [rawHtml, setRawHtml] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    if (!installPageUrl) return;

    setIsLoading(true);
    setError(null);

    fetch(installPageUrl, {
      headers: {
        "Gram-Session": session.session,
        "Gram-Project": project.slug,
        Accept: "text/html,application/xhtml+xml",
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

  // Add effect to scale iframe content
  useEffect(() => {
    if (iframeRef.current && rawHtml) {
      const iframe = iframeRef.current;

      const updateScale = () => {
        iframe.onload = () => {
          try {
            const doc = iframe.contentDocument;
            if (doc && doc.body) {
              const contentWidth = doc.body.scrollWidth;
              const contentHeight = doc.body.scrollHeight;
              const iframeWidth = iframe.offsetWidth * 0.7;
              const iframeHeight = iframe.offsetHeight;

              // Calculate scale based on both width and height constraints
              const widthScale = iframeWidth / contentWidth;
              const heightScale = iframeHeight / contentHeight;
              const scale = Math.min(widthScale, heightScale, 1); // Don't scale up, only down

              if (scale < 1) {
                iframe.style.transform = `scale(${scale})`;
                iframe.style.transformOrigin = "top left";
                iframe.style.width = `${100 / scale}%`;
                iframe.style.minHeight = `${height * (1 / scale)}px`;
              } else {
                iframe.style.transform = "scale(1)";
                iframe.style.width = "100%";
                iframe.style.height = "100%";
                iframe.style.minHeight = "100%";
              }
            }
          } catch (e) {
            console.warn("Could not access iframe content for scaling:", e);
          }
        };
      };

      updateScale();
    }
  }, [rawHtml]);

  if (error) {
    return <div className="text-red-600">Error loading page: {error}</div>;
  }

  if (isLoading) {
    return <div>Loading...</div>;
  }

  if (!toolset?.mcpSlug) {
    return <div>No MCP slug found</div>;
  }

  return (
    <div
      className={cn(
        `w-full max-h-[${height}px] border-1 rounded-lg overflow-hidden pointer-events-none`,
        className,
      )}
    >
      <iframe
        ref={iframeRef}
        title="MCP Hosted Preview"
        className="w-full flex-1"
        style={{
          height: `${height}px`,
          transformOrigin: "top left",
        }}
      />
    </div>
  );
}
