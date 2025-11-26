import { createReadStream, unlinkSync, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import Papa from "papaparse";
import { Pinecone } from "@pinecone-database/pinecone";

const ZIP_FILE = "../../assets/movies_dataset_with_embeddings.csv.zip";
const CSV_FILE = "dataset_with_embedtadings.csv";
const INDEX_NAME = "movies";

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

async function seedDatabase() {
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

    // Initialize Pinecone client (pointing to local index instance)
    const pc = new Pinecone({
      apiKey: process.env.PINECONE_API_KEY || "dummy-key",
    });

    console.log("Connected to Pinecone Local successfully");

    // Get index reference (pinecone-index provides a pre-configured index)
    // For local development, specify the host URL directly
    const index = pc.index(INDEX_NAME, "http://localhost:5081");

    // Check if data already exists
    try {
      const stats = await index.describeIndexStats();
      const recordCount = stats.totalRecordCount || 0;

      if (recordCount > 0) {
        console.log(
          `Index already contains ${recordCount} records. Skipping seed.`,
        );
        console.log("To re-seed, run: npm run db:reset");
        return;
      }
    } catch (error) {
      console.log("Could not check index stats, proceeding with seeding...");
    }

    console.log("Starting database seed...");

    let processed = 0;
    let inserted = 0;
    let skipped = 0;
    const batchSize = 200;
    let batch: any[] = [];

    // Helper function to upsert batch with retries
    const upsertBatch = async (records: any[]) => {
      if (records.length === 0) return;

      try {
        await index.upsert(records);
        inserted += records.length;
        console.log(`Inserted ${inserted} movies...`);
      } catch (error) {
        console.error("Error upserting batch:", error);
        throw error;
      }
    };

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
                console.warn(`Skipping malformed row ${processed + skipped}`);
              }
              return;
            }

            const row = result.data;
            processed++;

            // Parse embedding array
            const embeddingArray = JSON.parse(row.Embedding);

            // Validate and parse release date
            let releaseDate: string | null = null;
            if (row.Release_Date) {
              const dateStr = row.Release_Date.trim();
              if (/^\d{4}-\d{2}-\d{2}$/.test(dateStr)) {
                const parsed = new Date(dateStr);
                if (!isNaN(parsed.getTime())) {
                  releaseDate = dateStr;
                }
              }
            }

            // Validate and truncate original_language (max 10 chars)
            let originalLanguage: string | null = null;
            if (row.Original_Language) {
              const lang = row.Original_Language.trim();
              if (lang.length <= 10 && /^[a-z]{2}(-[A-Z]{2})?$/i.test(lang)) {
                originalLanguage = lang;
              }
            }

            // Build metadata object without null values
            const metadata: Record<string, string | number> = {
              title: row.Title,
            };
            if (row.Overview) metadata.overview = row.Overview;
            if (releaseDate) metadata.release_date = releaseDate;
            if (row.Genre) metadata.genre = row.Genre;
            if (row.Popularity && !isNaN(parseFloat(row.Popularity))) {
              metadata.popularity = parseFloat(row.Popularity);
            }
            if (row.Vote_Count && !isNaN(parseInt(row.Vote_Count))) {
              metadata.vote_count = parseInt(row.Vote_Count);
            }
            if (row.Vote_Average && !isNaN(parseFloat(row.Vote_Average))) {
              metadata.vote_average = parseFloat(row.Vote_Average);
            }
            if (originalLanguage) metadata.original_language = originalLanguage;
            if (row.Poster_Url) metadata.poster_url = row.Poster_Url;

            // Add to batch
            batch.push({
              id: `movie_${processed}`,
              values: embeddingArray,
              metadata,
            });

            // Insert batch when it reaches the batch size
            if (batch.length >= batchSize) {
              parser.pause();
              await upsertBatch(batch);
              batch = [];
              parser.resume();
            }
          } catch (err) {
            console.error("Error processing row:", err);
            parser.abort();
            reject(err);
          }
        },
        complete: async () => {
          try {
            // Insert remaining batch
            if (batch.length > 0) {
              await upsertBatch(batch);
            }

            const totalRows = processed + skipped;
            let message = `✓ Successfully seeded ${inserted} movies from ${totalRows} rows`;
            if (skipped > 0) {
              message += ` (${skipped} rows skipped due to parse errors)`;
            }
            console.log(message);
            resolve();
          } catch (err) {
            reject(err);
          }
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
}

seedDatabase();
