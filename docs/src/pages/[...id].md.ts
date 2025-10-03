import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

export async function getStaticPaths() {
  const docs = await getCollection("docs");
  return docs.map((entry) => {
    return {
      params: { id: entry.id },
    };
  });
}

export const GET: APIRoute = async function get({ params }) {
  if (!params.id?.trim()) {
    return new Response(null, {
      status: 404,
    });
  }

  const entry = (await getCollection("docs")).find((e) => e.id === params.id);
  if (!entry) {
    return new Response(null, {
      status: 404,
    });
  }

  const content = [
    `# ${entry.data.title}`,
    entry.data.description ? `> ${entry.data.description}` : "",
    entry.body,
  ]
    .filter(Boolean)
    .join("\n\n");

  return new Response(content, {
    headers: {
      "Content-Type": "text/markdown",
    },
  });
};
