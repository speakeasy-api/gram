import { createReadStream, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import Papa from "papaparse";
import { createClient, ClickHouseClient } from "@clickhouse/client-web";

const ZIP_FILE = "../../assets/chinook_dataset.csv.zip";

interface TableInfo {
  name: string;
  csvFile: string;
  createTableSQL: string;
  columnMapping: string[];
  batchSize?: number;
}

const TABLES: TableInfo[] = [
  {
    name: "Artist",
    csvFile: "chinook_dataset.csv/Artist.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Artist (
        ArtistId UInt32,
        Name String
      ) ENGINE = MergeTree()
      ORDER BY ArtistId
    `,
    columnMapping: ["ArtistId", "Name"],
  },
  {
    name: "Album",
    csvFile: "chinook_dataset.csv/Album.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Album (
        AlbumId UInt32,
        Title String,
        ArtistId UInt32
      ) ENGINE = MergeTree()
      ORDER BY AlbumId
    `,
    columnMapping: ["AlbumId", "Title", "ArtistId"],
  },
  {
    name: "MediaType",
    csvFile: "chinook_dataset.csv/MediaType.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS MediaType (
        MediaTypeId UInt32,
        Name String
      ) ENGINE = MergeTree()
      ORDER BY MediaTypeId
    `,
    columnMapping: ["MediaTypeId", "Name"],
  },
  {
    name: "Genre",
    csvFile: "chinook_dataset.csv/Genre.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Genre (
        GenreId UInt32,
        Name String
      ) ENGINE = MergeTree()
      ORDER BY GenreId
    `,
    columnMapping: ["GenreId", "Name"],
  },
  {
    name: "Track",
    csvFile: "chinook_dataset.csv/Track.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Track (
        TrackId UInt32,
        Name String,
        AlbumId UInt32,
        MediaTypeId UInt32,
        GenreId UInt32,
        Composer String,
        Milliseconds UInt32,
        Bytes UInt32,
        UnitPrice Decimal(10,2)
      ) ENGINE = MergeTree()
      ORDER BY TrackId
    `,
    columnMapping: [
      "TrackId",
      "Name",
      "AlbumId",
      "MediaTypeId",
      "GenreId",
      "Composer",
      "Milliseconds",
      "Bytes",
      "UnitPrice",
    ],
  },
  {
    name: "Employee",
    csvFile: "chinook_dataset.csv/Employee.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Employee (
        EmployeeId UInt32,
        LastName String,
        FirstName String,
        Title String,
        ReportsTo Nullable(UInt32),
        BirthDate Nullable(DateTime),
        HireDate Nullable(DateTime),
        Address String,
        City String,
        State String,
        Country String,
        PostalCode String,
        Phone String,
        Fax String,
        Email String
      ) ENGINE = MergeTree()
      ORDER BY EmployeeId
    `,
    columnMapping: [
      "EmployeeId",
      "LastName",
      "FirstName",
      "Title",
      "ReportsTo",
      "BirthDate",
      "HireDate",
      "Address",
      "City",
      "State",
      "Country",
      "PostalCode",
      "Phone",
      "Fax",
      "Email",
    ],
  },
  {
    name: "Customer",
    csvFile: "chinook_dataset.csv/Customer.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Customer (
        CustomerId UInt32,
        FirstName String,
        LastName String,
        Company String,
        Address String,
        City String,
        State String,
        Country String,
        PostalCode String,
        Phone String,
        Fax String,
        Email String,
        SupportRepId Nullable(UInt32)
      ) ENGINE = MergeTree()
      ORDER BY CustomerId
    `,
    columnMapping: [
      "CustomerId",
      "FirstName",
      "LastName",
      "Company",
      "Address",
      "City",
      "State",
      "Country",
      "PostalCode",
      "Phone",
      "Fax",
      "Email",
      "SupportRepId",
    ],
  },
  {
    name: "Invoice",
    csvFile: "chinook_dataset.csv/Invoice.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Invoice (
        InvoiceId UInt32,
        CustomerId UInt32,
        InvoiceDate DateTime,
        BillingAddress String,
        BillingCity String,
        BillingState String,
        BillingCountry String,
        BillingPostalCode String,
        Total Decimal(10,2)
      ) ENGINE = MergeTree()
      ORDER BY InvoiceId
    `,
    columnMapping: [
      "InvoiceId",
      "CustomerId",
      "InvoiceDate",
      "BillingAddress",
      "BillingCity",
      "BillingState",
      "BillingCountry",
      "BillingPostalCode",
      "Total",
    ],
  },
  {
    name: "InvoiceLine",
    csvFile: "chinook_dataset.csv/InvoiceLine.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS InvoiceLine (
        InvoiceLineId UInt32,
        InvoiceId UInt32,
        TrackId UInt32,
        UnitPrice Decimal(10,2),
        Quantity UInt32
      ) ENGINE = MergeTree()
      ORDER BY InvoiceLineId
    `,
    columnMapping: [
      "InvoiceLineId",
      "InvoiceId",
      "TrackId",
      "UnitPrice",
      "Quantity",
    ],
  },
  {
    name: "Playlist",
    csvFile: "chinook_dataset.csv/Playlist.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Playlist (
        PlaylistId UInt32,
        Name String
      ) ENGINE = MergeTree()
      ORDER BY PlaylistId
    `,
    columnMapping: ["PlaylistId", "Name"],
  },
  {
    name: "PlaylistTrack",
    csvFile: "chinook_dataset.csv/PlaylistTrack.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS PlaylistTrack (
        PlaylistId UInt32,
        TrackId UInt32
      ) ENGINE = MergeTree()
      ORDER BY (PlaylistId, TrackId)
    `,
    columnMapping: ["PlaylistId", "TrackId"],
  },
  {
    name: "TrackPlays",
    csvFile: "chinook_dataset.csv/TrackPlays.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS TrackPlays (
        Id UUID,
        Date Date,
        UserId UUID,
        TrackId UInt32
      ) ENGINE = MergeTree()
      PARTITION BY toYYYYMM(Date)
      ORDER BY (Date, UserId, TrackId)
      SETTINGS index_granularity = 8192
    `,
    columnMapping: ["Id", "Date", "UserId", "TrackId"],
    batchSize: 10000,
  },
];

async function seedDatabase() {
  const host = process.env.CLICKHOUSE_HOST || "localhost";
  const port = process.env.CLICKHOUSE_PORT || "8124";
  const username = process.env.CLICKHOUSE_USERNAME || "gram_user";
  const password = process.env.CLICKHOUSE_PASSWORD || "gram_password";
  const database = process.env.CLICKHOUSE_DATABASE || "gram_example";

  const client = createClient({
    url: `http://${host}:${port}`,
    username,
    password,
    database,
  });

  try {
    // Test connection
    await client.query({ query: "SELECT 1", format: "JSONEachRow" });
    console.log("✓ Connected to ClickHouse database successfully");

    // Extract the zip file
    console.log("\n📦 Extracting Chinook dataset from zip file...");
    execSync(`unzip -o ${ZIP_FILE}`, { stdio: "inherit" });
    console.log("✓ Dataset extracted successfully");

    // Check if data already exists
    try {
      const resultSet = await client.query({
        query: "SELECT COUNT(*) as count FROM Artist",
        format: "JSONEachRow",
      });
      const data = await resultSet.json<{ count: string }>();
      const count = Number(data[0]?.count);

      if (count > 0) {
        console.log(
          `\n⚠️  Database already contains ${count} artists. Skipping seed.`,
        );
        console.log(
          "To re-seed, drop the tables first and run this script again.",
        );
        return;
      }
    } catch (error) {
      // Table doesn't exist yet, which is fine
      console.log("📊 Creating tables...");
    }

    // Create tables
    for (const table of TABLES) {
      console.log(`  Creating table: ${table.name}`);
      await client.command({ query: table.createTableSQL });
    }
    console.log("✓ All tables created successfully\n");

    // Seed each table
    for (const table of TABLES) {
      console.log(`📥 Seeding table: ${table.name}`);
      await seedTable(client, table);
    }

    console.log("\n✨ Database seeded successfully!");
  } catch (error) {
    console.error("\n❌ Error seeding database:", error);
    process.exit(1);
  } finally {
    // Clean up extracted CSV files
    console.log("\n🧹 Cleaning up extracted files...");
    if (existsSync("chinook_dataset.csv")) {
      execSync("rm -rf chinook_dataset.csv", { stdio: "pipe" });
    }
    if (existsSync("__MACOSX")) {
      execSync("rm -rf __MACOSX", { stdio: "pipe" });
    }
    console.log("✓ Cleanup complete");
    await client.close();
  }
}

async function seedTable(
  client: ClickHouseClient,
  table: TableInfo,
): Promise<void> {
  let inserted = 0;
  const batchSize = table.batchSize || 1000;
  let batch: any[] = [];

  await new Promise<void>((resolve, reject) => {
    const stream = createReadStream(table.csvFile);

    Papa.parse(stream, {
      header: true,
      skipEmptyLines: true,
      step: async (result, parser) => {
        try {
          const row = result.data as Record<string, string>;

          // Map columns and convert empty strings to null or appropriate values
          const record: Record<string, any> = {};
          for (const col of table.columnMapping) {
            let value = row[col];

            // Handle empty strings
            if (value === "" || value === undefined) {
              // For TrackPlays, TrackId should be a number
              if (table.name === "TrackPlays" && col === "TrackId") {
                record[col] = 0;
              } else {
                record[col] = null;
              }
            } else if (table.name === "TrackPlays" && col === "TrackId") {
              record[col] = parseInt(value, 10);
            } else {
              record[col] = value;
            }
          }

          batch.push(record);

          // Insert batch when it reaches the batch size
          if (batch.length >= batchSize) {
            parser.pause();

            await client.insert({
              table: table.name,
              values: batch,
              format: "JSONEachRow",
            });

            inserted += batch.length;
            console.log(`  Inserted ${inserted} rows...`);
            batch = [];

            parser.resume();
          }
        } catch (err) {
          console.error(`Error processing row in ${table.name}:`, err);
          parser.abort();
          reject(err);
        }
      },
      complete: async () => {
        try {
          // Insert remaining batch
          if (batch.length > 0) {
            await client.insert({
              table: table.name,
              values: batch,
              format: "JSONEachRow",
            });

            inserted += batch.length;
          }

          console.log(`✓ Inserted ${inserted} rows into ${table.name}\n`);
          resolve();
        } catch (err) {
          reject(err);
        }
      },
      error: (err) => {
        console.error(`CSV parsing error for ${table.name}:`, err);
        reject(err);
      },
    });
  });
}

seedDatabase();
