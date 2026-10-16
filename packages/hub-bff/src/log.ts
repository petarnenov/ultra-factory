// По един JSON обект на ред в stdout, което индексира log viewer-ът на Railway.
// Извикващите подават само полета, които съзнателно логват: никога тяло на
// заявка, cookie, Authorization header, парола или хеш на парола.
export type LogLevel = "info" | "warn" | "error";
export type LogFields = Record<string, string | number | boolean | undefined>;
export type Logger = (level: LogLevel, msg: string, fields?: LogFields) => void;

export function jsonLogger(write: (line: string) => void = (line) => process.stdout.write(line)): Logger {
	return (level, msg, fields) => {
		write(`${JSON.stringify({ ts: new Date().toISOString(), level, msg, ...fields })}\n`);
	};
}
