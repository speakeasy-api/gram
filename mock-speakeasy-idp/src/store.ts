import { randomUUID } from "crypto";

interface AuthCodeEntry {
  userId: string;
  createdAt: Date;
}

interface TokenEntry {
  userId: string;
  createdAt: Date;
}

const authCodes = new Map<string, AuthCodeEntry>();
const tokens = new Map<string, TokenEntry>();

export function generateAuthCode(userId: string): string {
  const code = randomUUID();
  authCodes.set(code, { userId, createdAt: new Date() });
  return code;
}

export function validateAuthCode(code: string): string | null {
  const entry = authCodes.get(code);
  if (!entry) {
    return null;
  }
  authCodes.delete(code);
  return entry.userId;
}

export function generateToken(userId: string): string {
  const token = randomUUID();
  tokens.set(token, { userId, createdAt: new Date() });
  return token;
}

export function validateToken(token: string): string | null {
  const entry = tokens.get(token);
  if (!entry) {
    return null;
  }
  return entry.userId;
}

export function revokeToken(token: string): void {
  tokens.delete(token);
}
