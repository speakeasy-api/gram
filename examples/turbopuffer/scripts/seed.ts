import { createReadStream, unlinkSync, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import Papa from "papaparse";
import Turbopuffer from "@turbopuffer/turbopuffer";

const TURBOPUFFER_REGION = process.env.TURBOPUFFER_REGION || "gcp-us-central1";
const ZIP_FILE = "../../assets/movies_dataset_with_embeddings.csv.zip";
const CSV_FILE = "dataset_with_embeddings.csv";
const NAMESPACE = "movies";
const BATCH_SIZE = 100;

const TURBOPUFFER_NS_SCHEMA: Turbopuffer.NamespaceWriteParams["schema"] = {
  popularity: "float",
  vote_average: "float",
};

const TURBOPUFFER_DISTANCE_METRIC: Turbopuffer.DistanceMetric =
  "cosine_distance";

interface MovieRow {
  Release_Date: string;
  Title: string;
  Overview: string;
  Popularity: string;
  Vote_Count: string;
  Vote_Average: string;
  Original_Language: string;
  Genre: string;
  Poster_Url: string;
  Embedding: string;
}

type TurbopufferMovieRecord = {
  id: number;
  vector: number[];
  release_date: string;
  title: string;
  overview: string | null;
  popularity: number | null;
  vote_count: number | null;
  vote_average: number | null;
  original_language: string;
  genre: string | null;
  poster_url: string | null;
};

async function seedDatabase() {
  // Initialize Turbopuffer client
  const tpuf = new Turbopuffer({
    region: TURBOPUFFER_REGION,
    apiKey: process.env.TURBOPUFFER_API_KEY || "",
  });
  const ns = tpuf.namespace(NAMESPACE);

  // Check if namespace already has data
  try {
    const metadata = await ns.metadata();
    const count = metadata.approx_row_count || 0;

    if (count > 0) {
      console.log(
        `Namespace "${NAMESPACE}" already contains ${count} movies. Skipping seed.`,
      );
      console.log(
        "To re-seed, delete the namespace in turbopuffer dashboard and run again.",
      );
      return;
    }
  } catch (error) {
    // Namespace doesn't exist yet, which is fine - we'll create it
    console.log("Namespace doesn't exist yet. Creating and seeding...");
  }

  // Extract and parse the CSV file
  const data: TurbopufferMovieRecord[] = [];
  try {
    // Clean up any existing extracted files
    if (existsSync(CSV_FILE)) {
      unlinkSync(CSV_FILE);
    }
    if (existsSync("__MACOSX")) {
      execSync("rm -rf __MACOSX", { stdio: "inherit" });
    }

    // Extract the zip file
    console.log("Extracting dataset from zip file...");
    execSync(`unzip -o ${ZIP_FILE}`, { stdio: "inherit" });
    console.log("Dataset extracted successfully");

    let processed = 0;
    let skipped = 0;

    await new Promise<void>((resolve, reject) => {
      const stream = createReadStream(CSV_FILE);

      Papa.parse<MovieRow>(stream, {
        header: true,
        skipEmptyLines: true,
        quoteChar: '"',
        escapeChar: '"',
        step: async (result, parser) => {
          try {
            if (result.errors.length > 0) {
              skipped++;
              if (skipped <= 5) {
                // Only log the first 5 errors to avoid spam
                console.warn(`Skipping malformed row ${processed + skipped}`);
              }
              return;
            }

            const row = result.data;
            processed++;

            // Parse embedding array
            const embeddingArray: number[] = JSON.parse(row.Embedding);

            // Validate and parse release date
            let releaseDate: string | null = null;
            if (row.Release_Date) {
              const dateStr = row.Release_Date.trim();
              // Check if it's a valid date format (YYYY-MM-DD)
              if (/^\d{4}-\d{2}-\d{2}$/.test(dateStr)) {
                const parsed = new Date(dateStr);
                // Verify it's a valid date
                if (!isNaN(parsed.getTime())) {
                  releaseDate = dateStr;
                }
              }
            }

            // Validate and truncate original_language (max 10 chars)
            let originalLanguage: string | null = null;
            if (row.Original_Language) {
              const lang = row.Original_Language.trim();
              // Only accept if it looks like a language code (short alphanumeric string)
              if (lang.length <= 10 && /^[a-z]{2}(-[A-Z]{2})?$/i.test(lang)) {
                originalLanguage = lang;
              }
            }

            data.push({
              id: data.length,
              release_date: releaseDate || "",
              title: row.Title,
              overview: row.Overview || null,
              popularity: parseFloat(row.Popularity) || null,
              vote_count: parseInt(row.Vote_Count) || null,
              vote_average: parseFloat(row.Vote_Average) || null,
              original_language: originalLanguage || "unknown",
              genre: row.Genre || null,
              poster_url: row.Poster_Url || null,
              vector: embeddingArray,
            });
          } catch (err) {
            console.error("Error processing row:", err);
            parser.abort();
            reject(err);
          }
        },
        complete: () => {
          resolve();
        },
        error: (err) => {
          console.error("CSV parsing error:", err);
          reject(err);
        },
      });
    });
  } catch (err) {
    console.error("Error seeding database:", err);
    process.exit(1);
  } finally {
    // Clean up the extracted files
    if (existsSync(CSV_FILE)) {
      console.log("Cleaning up extracted files...");
      unlinkSync(CSV_FILE);
    }
    if (existsSync("__MACOSX")) {
      execSync("rm -rf __MACOSX", { stdio: "pipe" });
    }
    console.log("Cleanup complete");
  }

  console.log("Inserting data into Turbopuffer...");
  for (let i = 0; i < data.length; i += BATCH_SIZE) {
    const batch = data.slice(i, i + BATCH_SIZE);
    try {
      await ns.write({
        upsert_rows: batch,
        distance_metric: TURBOPUFFER_DISTANCE_METRIC,
        schema: TURBOPUFFER_NS_SCHEMA,
      });
      console.log(`Inserted ${i + BATCH_SIZE} of ${data.length} movies...`);
    } catch (err) {
      console.error("Error inserting batch:", err);
      process.exit(1);
    }
  }
}

seedDatabase();
