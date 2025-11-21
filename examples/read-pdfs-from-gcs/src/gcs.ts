import { Storage } from "@google-cloud/storage";

export interface GCSFile {
  name: string;
  fullPath: string;
  size: number;
  updated: string;
}

export async function listFiles(bucketName: string): Promise<GCSFile[]> {
  const storage = new Storage();
  const bucket = storage.bucket(bucketName);
  const [files] = await bucket.getFiles();

  return files.map((file) => ({
    name: file.name.split("/").pop() || file.name,
    fullPath: `gs://${bucketName}/${file.name}`,
    size: Number(file.metadata.size || 0),
    updated: file.metadata.updated || "",
  }));
}

export function searchFiles(files: GCSFile[], query: string): GCSFile[] {
  const normalize = (str: string) =>
    str
      .toLowerCase()
      .replaceAll(" ", "")
      .replaceAll("-", "")
      .replaceAll("_", "");
  const lowerQuery = normalize(query);
  return files.filter((file) => normalize(file.name).includes(lowerQuery));
}

export async function downloadFile(filePath: string): Promise<Buffer> {
  const storage = new Storage();

  const { bucketName, rest } = parseFilePath(filePath);

  const bucket = storage.bucket(bucketName);
  const file = bucket.file(rest || "");

  const [buffer] = await file.download();
  return buffer;
}

export function parseFilePath(filePath: string): {
  bucketName: string;
  rest?: string;
} {
  const pathMatch = filePath.match(/^gs:\/\/([^\/]+)\/?(.+)$/);
  if (!pathMatch || !pathMatch[1] || !pathMatch[2]) {
    throw new Error(
      `Invalid GCS file path format: ${filePath}. Expected format: gs://bucket-name/path/to/file.pdf`
    );
  }
  return { bucketName: pathMatch[1], rest: pathMatch[2] };
}
