// Prints an AO_WEB_PASSWORD_HASH value for the password read from stdin:
//   printf '%s' 'the password' | npm run --silent hash-password
import { hashPassword } from "../src/password.ts";

const chunks: Buffer[] = [];
for await (const chunk of process.stdin) chunks.push(chunk as Buffer);
const password = Buffer.concat(chunks).toString("utf8").replace(/\r?\n$/, "");
if (!password) {
	process.stderr.write("usage: printf '%s' '<password>' | npm run --silent hash-password\n");
	process.exit(2);
}
process.stdout.write(`${await hashPassword(password)}\n`);
