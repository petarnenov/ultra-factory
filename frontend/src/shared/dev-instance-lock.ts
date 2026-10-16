// Message for a dev launch that lost requestSingleInstanceLock().
//
// Dev runs share one Chromium profile (~/.ao/dev/electron), so a dev app left
// running (often orphaned after electron-forge exits) makes every later
// `make dev` exit at once. Electron says nothing about it and forge still
// prints "Launched Electron app", so the failure reads as a hang. This names
// the holder and the two ways out.
//
// Kept side-effect free (no node:* or electron imports) so it runs in vitest.

/**
 * Pulls the holder PID out of Chromium's SingletonLock symlink target, which is
 * "<hostname>-<pid>". Returns null when the target is missing or unparseable.
 */
export function singletonLockHolderPid(lockTarget: string | null): number | null {
	const match = lockTarget?.match(/-(\d+)$/);
	return match ? Number(match[1]) : null;
}

export function devInstanceLockMessage(userDataDir: string, holderPid: number | null): string {
	const holder = holderPid === null ? "another AO dev instance" : `another AO dev instance (pid ${holderPid})`;
	const quit = holderPid === null ? "Quit it" : `Quit it (kill ${holderPid})`;
	return (
		`AO dev: ${holder} already holds the profile at ${userDataDir}, so this launch is exiting. ` +
		`${quit}, or set AO_DEV_ELECTRON_DIR to give this checkout its own profile.`
	);
}
