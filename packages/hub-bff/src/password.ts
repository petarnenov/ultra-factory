import { randomBytes, scrypt as scryptCallback, timingSafeEqual, type ScryptOptions } from "node:crypto";

// Operator password hashes use scrypt from node:crypto, encoded PHC-style as
// `$scrypt$ln=..,r=..,p=..$<salt>$<hash>` (unpadded base64url). See README.md.
const PREFIX = "scrypt";
const KEY_LENGTH = 32;
const SALT_LENGTH = 16;
// OWASP's scrypt baseline: N=2^17, r=8, p=1 (128 MiB per hash).
export const DEFAULT_COST = { ln: 17, r: 8, p: 1 } as const;

export type ScryptCost = { ln: number; r: number; p: number };

export type ParsedHash = ScryptCost & { salt: Buffer; hash: Buffer };

function scrypt(password: string, salt: Buffer, cost: ScryptCost): Promise<Buffer> {
	const N = 2 ** cost.ln;
	// Node rejects anything above 32 MiB unless maxmem is raised.
	const options: ScryptOptions = { N, r: cost.r, p: cost.p, maxmem: 128 * N * cost.r * cost.p + 1024 * 1024 };
	return new Promise((resolve, reject) => {
		scryptCallback(password.normalize("NFKC"), salt, KEY_LENGTH, options, (err, key) => (err ? reject(err) : resolve(key)));
	});
}

export async function hashPassword(password: string, cost: ScryptCost = DEFAULT_COST): Promise<string> {
	const salt = randomBytes(SALT_LENGTH);
	const hash = await scrypt(password, salt, cost);
	return `$${PREFIX}$ln=${cost.ln},r=${cost.r},p=${cost.p}$${salt.toString("base64url")}$${hash.toString("base64url")}`;
}

export function parsePasswordHash(encoded: string): ParsedHash {
	const parts = encoded.trim().split("$");
	if (parts.length !== 5 || parts[0] !== "" || parts[1] !== PREFIX) {
		throw new Error("password hash must be in the form $scrypt$ln=..,r=..,p=..$<salt>$<hash>");
	}
	const params = new Map(parts[2].split(",").map((kv) => kv.split("=") as [string, string]));
	const ln = Number(params.get("ln"));
	const r = Number(params.get("r"));
	const p = Number(params.get("p"));
	if (![ln, r, p].every((v) => Number.isInteger(v) && v > 0) || ln > 22) {
		throw new Error("password hash has invalid scrypt parameters");
	}
	const salt = Buffer.from(parts[3], "base64url");
	const hash = Buffer.from(parts[4], "base64url");
	if (salt.length < SALT_LENGTH || hash.length !== KEY_LENGTH) {
		throw new Error("password hash has an invalid salt or key length");
	}
	return { ln, r, p, salt, hash };
}

export async function verifyPassword(password: string, parsed: ParsedHash): Promise<boolean> {
	const candidate = await scrypt(password, parsed.salt, parsed);
	return timingSafeEqual(candidate, parsed.hash);
}
