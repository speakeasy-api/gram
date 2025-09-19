import type { APIRoute } from "astro";
import { getCollection } from "astro:content";
import { renderOGImage } from "../../lib/og-page";

export async function getStaticPaths() {
  const docs = await getCollection("docs");
  return docs.map((entry) => {
    return {
      params: { id: entry.id },
      props: entry,
    };
  });
}

export const GET: APIRoute = async function get({ params }) {
  const entry = (await getCollection("docs")).find((e) => e.id === params.id);
  if (!entry) {
    return new Response(null, {
      status: 404,
    });
  }

  const title = entry.id === "index" ? "Documentation" : entry.data.title;

  const png = await renderOGImage(title, entry.data.description);

  return new Response(png, {
    headers: {
      "Content-Type": "image/png",
    },
  });
};
