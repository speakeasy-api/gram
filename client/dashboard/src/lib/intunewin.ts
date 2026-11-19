import { zipSync } from "fflate";

// Builds a Win32 app package (.intunewin) in the browser, matching what
// Microsoft's IntuneWinAppUtil produces: an outer zip holding Detection.xml
// and the encrypted inner zip of the source files. The inner file is
// HMAC(32) || IV(16) || AES-256-CBC(zip), with the HMAC-SHA256 over IV and
// ciphertext. The keys ship in Detection.xml, so this protects integrity in
// transit to Intune, not secrecy.

export type IntuneWinInput = {
  setupFile: string;
  files: Record<string, Uint8Array>;
};

const TOOL_VERSION = "1.8.7.0";
const INNER_FILE_NAME = "IntunePackage.intunewin";

export async function buildIntuneWin(
  input: IntuneWinInput,
): Promise<Uint8Array> {
  if (!(input.setupFile in input.files)) {
    throw new Error(`setup file ${input.setupFile} is not in the package`);
  }

  const plain = zipSync(input.files, { level: 0 });
  const encryptionKey = randomBytes(32);
  const macKey = randomBytes(32);
  const iv = randomBytes(16);

  const subtle = globalThis.crypto.subtle;
  const aesKey = await subtle.importKey(
    "raw",
    encryptionKey,
    { name: "AES-CBC" },
    false,
    ["encrypt"],
  );
  const cipher = new Uint8Array(
    await subtle.encrypt({ name: "AES-CBC", iv }, aesKey, plain),
  );

  const hmacKey = await subtle.importKey(
    "raw",
    macKey,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const mac = new Uint8Array(
    await subtle.sign("HMAC", hmacKey, concat(iv, cipher)),
  );
  const digest = new Uint8Array(await subtle.digest("SHA-256", plain));

  const detection = detectionXml({
    name: input.setupFile,
    setupFile: input.setupFile,
    unencryptedContentSize: plain.length,
    encryptionKey,
    macKey,
    iv,
    mac,
    digest,
  });

  return zipSync(
    {
      [`IntuneWinPackage/Contents/${INNER_FILE_NAME}`]: concat(mac, iv, cipher),
      "IntuneWinPackage/Metadata/Detection.xml": new TextEncoder().encode(
        detection,
      ),
    },
    { level: 0 },
  );
}

function detectionXml(d: {
  name: string;
  setupFile: string;
  unencryptedContentSize: number;
  encryptionKey: Uint8Array;
  macKey: Uint8Array;
  iv: Uint8Array;
  mac: Uint8Array;
  digest: Uint8Array;
}): string {
  return `<ApplicationInfo xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" ToolVersion="${TOOL_VERSION}">
  <Name>${xmlEscape(d.name)}</Name>
  <UnencryptedContentSize>${d.unencryptedContentSize}</UnencryptedContentSize>
  <FileName>${INNER_FILE_NAME}</FileName>
  <SetupFile>${xmlEscape(d.setupFile)}</SetupFile>
  <EncryptionInfo>
    <EncryptionKey>${base64(d.encryptionKey)}</EncryptionKey>
    <MacKey>${base64(d.macKey)}</MacKey>
    <InitializationVector>${base64(d.iv)}</InitializationVector>
    <Mac>${base64(d.mac)}</Mac>
    <ProfileIdentifier>ProfileVersion1</ProfileIdentifier>
    <FileDigest>${base64(d.digest)}</FileDigest>
    <FileDigestAlgorithm>SHA256</FileDigestAlgorithm>
  </EncryptionInfo>
</ApplicationInfo>`;
}

function randomBytes(n: number): Uint8Array {
  return globalThis.crypto.getRandomValues(new Uint8Array(n));
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

function base64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

function xmlEscape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
