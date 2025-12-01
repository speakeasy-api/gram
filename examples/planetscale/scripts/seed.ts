import { createReadStream, unlinkSync, existsSync } from "node:fs";
import { execSync } from "node:child_process";
import Papa from "papaparse";
import ws from "ws";
import { Pool, neonConfig } from "@neondatabase/serverless";

// Configure Neon for PlanetScale compatibility
neonConfig.webSocketConstructor = ws;
neonConfig.pipelineConnect = false;
neonConfig.wsProxy = (host, port) => `${host}/v2?address=${host}:${port}`;

const ZIP_FILE = "../../assets/chinook_dataset.csv.zip";

interface TableInfo {
  name: string;
  csvFile: string;
  createTableSQL: string;
  columnMapping: string[];
}

const TABLES: TableInfo[] = [
  {
    name: "Artist",
    csvFile: "Artist.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Artist (
        ArtistId INT PRIMARY KEY,
        Name VARCHAR(120)
      )
    `,
    columnMapping: ["ArtistId", "Name"],
  },
  {
    name: "Album",
    csvFile: "Album.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Album (
        AlbumId INT PRIMARY KEY,
        Title VARCHAR(160) NOT NULL,
        ArtistId INT NOT NULL,
        FOREIGN KEY (ArtistId) REFERENCES Artist(ArtistId)
      )
    `,
    columnMapping: ["AlbumId", "Title", "ArtistId"],
  },
  {
    name: "MediaType",
    csvFile: "MediaType.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS MediaType (
        MediaTypeId INT PRIMARY KEY,
        Name VARCHAR(120)
      )
    `,
    columnMapping: ["MediaTypeId", "Name"],
  },
  {
    name: "Genre",
    csvFile: "Genre.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Genre (
        GenreId INT PRIMARY KEY,
        Name VARCHAR(120)
      )
    `,
    columnMapping: ["GenreId", "Name"],
  },
  {
    name: "Track",
    csvFile: "Track.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Track (
        TrackId INT PRIMARY KEY,
        Name VARCHAR(200) NOT NULL,
        AlbumId INT,
        MediaTypeId INT NOT NULL,
        GenreId INT,
        Composer VARCHAR(220),
        Milliseconds INT NOT NULL,
        Bytes INT,
        UnitPrice DECIMAL(10,2) NOT NULL,
        FOREIGN KEY (AlbumId) REFERENCES Album(AlbumId),
        FOREIGN KEY (MediaTypeId) REFERENCES MediaType(MediaTypeId),
        FOREIGN KEY (GenreId) REFERENCES Genre(GenreId)
      )
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
    csvFile: "Employee.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Employee (
        EmployeeId INT PRIMARY KEY,
        LastName VARCHAR(20) NOT NULL,
        FirstName VARCHAR(20) NOT NULL,
        Title VARCHAR(30),
        ReportsTo INT,
        BirthDate TIMESTAMP,
        HireDate TIMESTAMP,
        Address VARCHAR(70),
        City VARCHAR(40),
        State VARCHAR(40),
        Country VARCHAR(40),
        PostalCode VARCHAR(10),
        Phone VARCHAR(24),
        Fax VARCHAR(24),
        Email VARCHAR(60),
        FOREIGN KEY (ReportsTo) REFERENCES Employee(EmployeeId)
      )
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
    csvFile: "Customer.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Customer (
        CustomerId INT PRIMARY KEY,
        FirstName VARCHAR(40) NOT NULL,
        LastName VARCHAR(20) NOT NULL,
        Company VARCHAR(80),
        Address VARCHAR(70),
        City VARCHAR(40),
        State VARCHAR(40),
        Country VARCHAR(40),
        PostalCode VARCHAR(10),
        Phone VARCHAR(24),
        Fax VARCHAR(24),
        Email VARCHAR(60) NOT NULL,
        SupportRepId INT,
        FOREIGN KEY (SupportRepId) REFERENCES Employee(EmployeeId)
      )
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
    csvFile: "Invoice.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Invoice (
        InvoiceId INT PRIMARY KEY,
        CustomerId INT NOT NULL,
        InvoiceDate TIMESTAMP NOT NULL,
        BillingAddress VARCHAR(70),
        BillingCity VARCHAR(40),
        BillingState VARCHAR(40),
        BillingCountry VARCHAR(40),
        BillingPostalCode VARCHAR(10),
        Total DECIMAL(10,2) NOT NULL,
        FOREIGN KEY (CustomerId) REFERENCES Customer(CustomerId)
      )
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
    csvFile: "InvoiceLine.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS InvoiceLine (
        InvoiceLineId INT PRIMARY KEY,
        InvoiceId INT NOT NULL,
        TrackId INT NOT NULL,
        UnitPrice DECIMAL(10,2) NOT NULL,
        Quantity INT NOT NULL,
        FOREIGN KEY (InvoiceId) REFERENCES Invoice(InvoiceId),
        FOREIGN KEY (TrackId) REFERENCES Track(TrackId)
      )
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
    csvFile: "Playlist.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS Playlist (
        PlaylistId INT PRIMARY KEY,
        Name VARCHAR(120)
      )
    `,
    columnMapping: ["PlaylistId", "Name"],
  },
  {
    name: "PlaylistTrack",
    csvFile: "PlaylistTrack.csv",
    createTableSQL: `
      CREATE TABLE IF NOT EXISTS PlaylistTrack (
        PlaylistId INT NOT NULL,
        TrackId INT NOT NULL,
        PRIMARY KEY (PlaylistId, TrackId),
        FOREIGN KEY (PlaylistId) REFERENCES Playlist(PlaylistId),
        FOREIGN KEY (TrackId) REFERENCES Track(TrackId)
      )
    `,
    columnMapping: ["PlaylistId", "TrackId"],
  },
];

async function seedDatabase() {
  const host = process.env.PLANETSCALE_HOST;
  const username = process.env.PLANETSCALE_USERNAME;
  const password = process.env.PLANETSCALE_PASSWORD;
  const database = process.env.PLANETSCALE_DATABASE;
  const port = process.env.PLANETSCALE_PORT;

  if (!host || !username || !password || !database) {
    console.error(
      "Error: Missing required environment variables. Please set PLANETSCALE_HOST, PLANETSCALE_USERNAME, PLANETSCALE_PASSWORD, and PLANETSCALE_DATABASE.",
    );
    process.exit(1);
  }

  const connectionString = `postgresql://${username}:${password}@${host}:${port || "6432"}/${database}`;
  const pool = new Pool({ connectionString });

  try {
    // Test connection
    await pool.query("SELECT 1");
    console.log("✓ Connected to PlanetScale database successfully");

    // Extract the zip file
    console.log("\n📦 Extracting Chinook dataset from zip file...");
    execSync(`unzip -o ${ZIP_FILE}`, { stdio: "inherit" });
    console.log("✓ Dataset extracted successfully");

    // Check if data already exists
    try {
      const result = await pool.query("SELECT COUNT(*) as count FROM Artist");
      const count = Number(result.rows[0]?.count);

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
      await pool.query(table.createTableSQL);
    }
    console.log("✓ All tables created successfully\n");

    // Seed each table
    for (const table of TABLES) {
      console.log(`📥 Seeding table: ${table.name}`);
      await seedTable(pool, table);
    }

    console.log("\n✨ Database seeded successfully!");
  } catch (error) {
    console.error("\n❌ Error seeding database:", error);
    process.exit(1);
  } finally {
    // Clean up extracted CSV files
    console.log("\n🧹 Cleaning up extracted files...");
    for (const table of TABLES) {
      if (existsSync(table.csvFile)) {
        unlinkSync(table.csvFile);
      }
    }
    if (existsSync("__MACOSX")) {
      execSync("rm -rf __MACOSX", { stdio: "pipe" });
    }
    console.log("✓ Cleanup complete");
  }
}

async function seedTable(pool: Pool, table: TableInfo): Promise<void> {
  let inserted = 0;
  const batchSize = 100;
  let batch: any[][] = [];

  await new Promise<void>((resolve, reject) => {
    const stream = createReadStream(table.csvFile);

    Papa.parse(stream, {
      header: true,
      skipEmptyLines: true,
      step: async (result, parser) => {
        try {
          const row = result.data as Record<string, string>;

          // Map columns and convert empty strings to null
          const values = table.columnMapping.map((col) => {
            const value = row[col];
            return value === "" || value === undefined ? null : value;
          });

          batch.push(values);

          // Insert batch when it reaches the batch size
          if (batch.length >= batchSize) {
            parser.pause();

            let paramIndex = 1;
            const placeholders = batch
              .map(
                () =>
                  `(${table.columnMapping.map(() => `$${paramIndex++}`).join(", ")})`,
              )
              .join(", ");

            const params = batch.flat();

            await pool.query(
              `INSERT INTO ${table.name} (${table.columnMapping.join(", ")}) VALUES ${placeholders}`,
              params,
            );

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
            let paramIndex = 1;
            const placeholders = batch
              .map(
                () =>
                  `(${table.columnMapping.map(() => `$${paramIndex++}`).join(", ")})`,
              )
              .join(", ");

            const params = batch.flat();

            await pool.query(
              `INSERT INTO ${table.name} (${table.columnMapping.join(", ")}) VALUES ${placeholders}`,
              params,
            );

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
