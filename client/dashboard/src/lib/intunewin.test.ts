// @vitest-environment node
import { strFromU8, unzipSync } from "fflate";
import { describe, expect, it } from "vitest";
import { buildIntuneWin } from "./intunewin";

const tag = (xml: string, name: string) =>
  new RegExp(`<${name}>(.*?)</${name}>`).exec(xml)?.[1] ?? "";
function entry(files: Record<string, Uint8Array>, name: string): Uint8Array {
  const file = files[name];
  if (!file) throw new Error(`missing ${name}`);
  return file;
}
const b64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));

describe("buildIntuneWin", () => {
  it("produces a package Intune can verify and decrypt", async () => {
    const enc = new TextEncoder();
    const pkg = await buildIntuneWin({
      setupFile: "setup.ps1",
      files: {
        "setup.ps1": enc.encode("Write-Output 'hi'"),
        "data.bin": new Uint8Array(5000).fill(7),
      },
    });

    const outer = unzipSync(pkg);
    expect(Object.keys(outer).sort()).toEqual([
      "IntuneWinPackage/Contents/IntunePackage.intunewin",
      "IntuneWinPackage/Metadata/Detection.xml",
    ]);
    const xml = strFromU8(
      entry(outer, "IntuneWinPackage/Metadata/Detection.xml"),
    );
    expect(tag(xml, "Name")).toBe("setup.ps1");
    expect(tag(xml, "SetupFile")).toBe("setup.ps1");
    expect(tag(xml, "FileName")).toBe("IntunePackage.intunewin");
    expect(tag(xml, "ProfileIdentifier")).toBe("ProfileVersion1");
    expect(tag(xml, "FileDigestAlgorithm")).toBe("SHA256");

    const blob = entry(
      outer,
      "IntuneWinPackage/Contents/IntunePackage.intunewin",
    );
    const mac = blob.slice(0, 32);
    const iv = blob.slice(32, 48);
    expect(iv).toEqual(b64(tag(xml, "InitializationVector")));
    expect(mac).toEqual(b64(tag(xml, "Mac")));

    const subtle = globalThis.crypto.subtle;
    const hmacKey = await subtle.importKey(
      "raw",
      b64(tag(xml, "MacKey")),
      { name: "HMAC", hash: "SHA-256" },
      false,
      ["verify"],
    );
    expect(await subtle.verify("HMAC", hmacKey, mac, blob.slice(32))).toBe(
      true,
    );

    const aesKey = await subtle.importKey(
      "raw",
      b64(tag(xml, "EncryptionKey")),
      { name: "AES-CBC" },
      false,
      ["decrypt"],
    );
    const plain = new Uint8Array(
      await subtle.decrypt({ name: "AES-CBC", iv }, aesKey, blob.slice(48)),
    );
    expect(plain.length).toBe(Number(tag(xml, "UnencryptedContentSize")));
    expect(new Uint8Array(await subtle.digest("SHA-256", plain))).toEqual(
      b64(tag(xml, "FileDigest")),
    );

    const inner = unzipSync(plain);
    expect(strFromU8(entry(inner, "setup.ps1"))).toBe("Write-Output 'hi'");
    expect(entry(inner, "data.bin").length).toBe(5000);
  });

  it("rejects a setup file that isn't in the package", async () => {
    await expect(
      buildIntuneWin({ setupFile: "missing.exe", files: {} }),
    ).rejects.toThrow("missing.exe");
  });
});
