import { constants } from "node:fs";
import { mkdir, open, readFile, lstat } from "node:fs/promises";
import path from "node:path";

export type AuditEvent = "login.success" | "login.failure" | "host.add" | "host.remove" | "worker.create" | "worker.delete" | "worker.force_kill";
export type AuditEntry = { ts: string; event: AuditEvent; subjectId?: string };

export interface AuditLog {
	record(event: AuditEvent, subjectId?: string): Promise<void>;

	list(limit: number): Promise<AuditEntry[]>;
}

// openAuditLog stores one sanitized JSON object per line on the hub volume.
// Existing files with broad permissions or symlinks are refused.
export async function openAuditLog(file: string): Promise<AuditLog> {
	await mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
	let info = await lstat(file).catch((error: NodeJS.ErrnoException) => {
		if (error.code === "ENOENT") return undefined;
		throw error;
	});
	if (info?.isSymbolicLink()) throw new Error(`${file} must not be a symbolic link`);
	if (info && !info.isFile()) throw new Error(`${file} must be a regular file`);
	if (info && (info.mode & 0o077) !== 0) throw new Error(`${file} must not be readable or writable by group or others`);
	if (!info) {
		const created = await open(file, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_APPEND, 0o600);
		await created.close();
	}
	const handle = await open(file, constants.O_WRONLY | constants.O_APPEND | (constants.O_NOFOLLOW ?? 0));
	await handle.chmod(0o600);
	let queue = Promise.resolve();
	return {
		record(event, subjectId) {
			const entry: AuditEntry = { ts: new Date().toISOString(), event, ...(subjectId ? { subjectId } : {}) };
			const write = queue.then(() => handle.appendFile(`${JSON.stringify(entry)}\n`, { encoding: "utf8" }));
			queue = write.then(() => undefined, () => undefined);
			return write;
		},
		async list(limit) {
			await queue;
			const contents = await readFile(file, "utf8");
			return contents.split("\n").filter(Boolean).slice(-limit).map((line) => JSON.parse(line) as AuditEntry);
		},
	};
}
