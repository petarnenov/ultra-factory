dependency_freshness() {
	local dir="$1"
	local lockfile="$dir/package-lock.json"
	local installed_lock="$dir/node_modules/.package-lock.json"

	if [ ! -d "$dir/node_modules" ]; then
		printf 'missing\n'
	elif [ ! -f "$installed_lock" ] || [ "$lockfile" -nt "$installed_lock" ]; then
		printf 'stale\n'
	else
		printf 'fresh\n'
	fi
}
