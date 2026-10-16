// One JSON object per line on stdout, which is what Railway's log viewer
// indexes. Callers pass only fields they have chosen to log: never a request
// body, a cookie, an Authorization header, a password, or a password hash.
export type LogLevel = "info" | "warn" | "error";
export type LogFields = Record<string, string | number | boolean | undefined>;
export type Logger = (level: LogLevel, msg: string, fields?: LogFields) => void;

export function jsonLogger(write: (line: string) => void = (line) => process.stdout.write(line)): Logger {
	return (level, msg, fields) => {
		write(`${JSON.stringify({ ts: new Date().toISOString(), level, msg, ...fields })}\n`);
	};
}
