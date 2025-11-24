import { createReadStream, unlinkSync, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import Papa from "papaparse";
import pg from "pg";

const { Pool } = pg;

const ZIP_FILE = "dataset_with_embeddings.csv.zip";
const CSV_FILE = "dataset_with_embeddings.csv";

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
  const pool = new Pool({
    connectionString:
      process.env.DATABASE_URL ||
      "postgresql://postgres:postgres@localhost:5432/movies",
  });

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

    // Test connection
    await pool.query("SELECT 1");
    console.log("Connected to database successfully");

    // Check if data already exists
    const { rows } = await pool.query("SELECT COUNT(*) FROM movies");
    const count = parseInt(rows[0].count);

    if (count > 0) {
      console.log(`Database already contains ${count} movies. Skipping seed.`);
      console.log("To re-seed, run: npm run db:reset");
      await pool.end();
      return;
    }

    console.log("Starting database seed...");

    let processed = 0;
    let inserted = 0;
    let skipped = 0;
    const batchSize = 100;
    let batch: any[] = [];

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
            const embeddingArray = JSON.parse(row.Embedding);

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

            batch.push({
              releaseDate,
              title: row.Title,
              overview: row.Overview || null,
              popularity: parseFloat(row.Popularity) || null,
              voteCount: parseInt(row.Vote_Count) || null,
              voteAverage: parseFloat(row.Vote_Average) || null,
              originalLanguage,
              genre: row.Genre || null,
              posterUrl: row.Poster_Url || null,
              embedding: `[${embeddingArray.join(",")}]`,
            });

            // Insert batch when it reaches the batch size
            if (batch.length >= batchSize) {
              parser.pause();

              const values = batch
                .map(
                  (_, i) =>
                    `($${i * 10 + 1}, $${i * 10 + 2}, $${i * 10 + 3}, $${i * 10 + 4}, $${i * 10 + 5}, $${i * 10 + 6}, $${i * 10 + 7}, $${i * 10 + 8}, $${i * 10 + 9}, $${i * 10 + 10})`,
                )
                .join(", ");

              const params = batch.flatMap((item) => [
                item.releaseDate,
                item.title,
                item.overview,
                item.popularity,
                item.voteCount,
                item.voteAverage,
                item.originalLanguage,
                item.genre,
                item.posterUrl,
                item.embedding,
              ]);

              await pool.query(
                `INSERT INTO movies (release_date, title, overview, popularity, vote_count, vote_average, original_language, genre, poster_url, embedding) VALUES ${values}`,
                params,
              );

              inserted += batch.length;
              console.log(`Inserted ${inserted} movies...`);
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
              const values = batch
                .map(
                  (_, i) =>
                    `($${i * 10 + 1}, $${i * 10 + 2}, $${i * 10 + 3}, $${i * 10 + 4}, $${i * 10 + 5}, $${i * 10 + 6}, $${i * 10 + 7}, $${i * 10 + 8}, $${i * 10 + 9}, $${i * 10 + 10})`,
                )
                .join(", ");

              const params = batch.flatMap((item) => [
                item.releaseDate,
                item.title,
                item.overview,
                item.popularity,
                item.voteCount,
                item.voteAverage,
                item.originalLanguage,
                item.genre,
                item.posterUrl,
                item.embedding,
              ]);

              await pool.query(
                `INSERT INTO movies (release_date, title, overview, popularity, vote_count, vote_average, original_language, genre, poster_url, embedding) VALUES ${values}`,
                params,
              );

              inserted += batch.length;
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

    await pool.end();
  } catch (err) {
    console.error("Error seeding database:", err);
    await pool.end();
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
