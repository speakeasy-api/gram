import { Turbopuffer } from "@turbopuffer/turbopuffer";

async function deleteMoviesNamespace() {
  const tpuf = new Turbopuffer({
    region: process.env["TURBOPUFFER_REGION"]!,
    apiKey: process.env["TURBOPUFFER_API_KEY"]!,
  });

  const ns = tpuf.namespace("movies");
  const result = await ns.deleteAll();

  if (result.status !== "OK") {
    console.error(`Failed to delete namespace "movies"`);
  } else {
    console.log(`Successfully deleted all records in namespace "movies"`);
  }
}

deleteMoviesNamespace();
