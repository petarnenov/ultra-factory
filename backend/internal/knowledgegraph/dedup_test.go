package knowledgegraph

import "testing"

// The pairs come from bge-m3 calibration: each "different" pair scored
// as close as the paraphrases, so only sameClaim keeps them apart.
func TestSameClaim(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "paraphrase", a: "make ci-check fails with 'parallel golangci-lint is running'; wait and rerun.", b: "If make ci-check reports that a parallel golangci-lint is running, wait until it finishes.", want: true},
		{name: "same literal in another sentence", a: "Use CGO_ENABLED=0 when building the daemon for Linux.", b: "Build the ao daemon for Linux with CGO_ENABLED=0.", want: true},
		{name: "both negated", a: "Electron does not respawn a daemon killed from outside.", b: "A daemon killed externally isn't restarted by Electron.", want: true},
		{name: "curly apostrophe negation", a: "Electron does not respawn a killed daemon.", b: "Electron doesn’t respawn a killed daemon.", want: true},
		{name: "negation added", a: "Use CGO_ENABLED=0 when cross-compiling the daemon for Linux.", b: "Do not use CGO_ENABLED=0 when cross-compiling the daemon for Linux.", want: false},
		{name: "must versus must not", a: "The embed loop must run against the shared graph in live tests.", b: "The embed loop must not run against the shared graph in live tests.", want: false},
		{name: "always versus never", a: "Always rebase onto dev before opening the PR.", b: "Never rebase onto dev before opening the PR; merge dev instead.", want: false},
		{name: "changed number", a: "Set minScore to 0.70.", b: "Set minScore to 0.90.", want: false},
		{name: "changed file", a: "Regenerate openapi.yaml after a DTO change.", b: "Regenerate schema.ts after a DTO change.", want: false},
		{name: "same code span", a: "Run `go test -race` before the PR.", b: "Always run `go test -race` before opening the PR.", want: true},
		{name: "reordered literals stay apart", a: "Run `go test -race` before the PR.", b: "Before opening the PR, run `go test -race`.", want: false},
		{name: "changed long flag", a: "Recover with git push --force.", b: "Recover with git push --force-with-lease.", want: false},
		{name: "changed short flag", a: "Use the -race flag.", b: "Use the -short flag.", want: false},
		{name: "changed code span", a: "Run `go test` first.", b: "Run `go vet` first.", want: false},
		{name: "changed camel case identifier", a: "Use useQuery for the list.", b: "Use useMutation for the list.", want: false},
		{name: "quoted negation", a: "Workers should 'never' push to main.", b: "Workers should push to main.", want: false},
		{name: "swapped code spans", a: "Use `pnpm`, not `npm`, in the frontend.", b: "Use `npm`, not `pnpm`, in the frontend.", want: false},
		{name: "changed kebab case name", a: "Run make kg-deploy after changing the schema.", b: "Run make kg-reset after changing the schema.", want: false},
		{name: "changed all caps name", a: "Set GOPATH before building.", b: "Set GOROOT before building.", want: false},
		{name: "changed acronym identifier", a: "Use HTTPServer for this.", b: "Use HTTPClient for this.", want: false},
		{name: "bulgarian negation", a: "Не използвай --force при push.", b: "Използвай --force при push.", want: false},
		{name: "negator inside a flag", a: "Never run `git push --no-verify` on main.", b: "Run `git push --no-verify` on main.", want: false},
		{name: "negation moved to another value", a: "Use `pnpm`, not `npm`, in the frontend.", b: "Do not use `pnpm`; use `npm` in the frontend.", want: false},
		{name: "extra negation", a: "Run make test without -race.", b: "Run make test with -race, never without it.", want: false},
		{name: "apostrophe-less negation", a: "Electron doesnt respawn a killed daemon.", b: "Electron respawns a killed daemon.", want: false},
		{name: "markdown bold negation", a: "Do **not** run make sqlc before tests.", b: "Do run make sqlc before tests.", want: false},
		{name: "markdown italic negation", a: "*Never* restart the daemon.", b: "Always restart the daemon.", want: false},
		{name: "typographic quoted negation", a: "Use “not” here.", b: "Use here.", want: false},
		{name: "bulgarian quoted negation", a: "Пиши „не“ тук.", b: "Пиши тук.", want: false},
		{name: "none as negation", a: "None of the jobs need -race.", b: "All of the jobs need -race.", want: false},
		{name: "code span in parentheses", a: "Run tests on the dev branch (`dev`).", b: "Run tests on the dev branch (`main`).", want: false},
		{name: "bold code span", a: "Use **`pnpm`** in the frontend.", b: "Use **`npm`** in the frontend.", want: false},
		{name: "linked code span", a: "Use [`pnpm`](link) here.", b: "Use [`npm`](link) here.", want: false},
		{name: "code span case", a: "Call `Foo` from the handler.", b: "Call `foo` from the handler.", want: false},
		{name: "acute accent apostrophe", a: "Electron doesn´t respawn a killed daemon.", b: "Electron respawns a killed daemon.", want: false},
		{name: "backtick apostrophe", a: "Don`t restart the daemon.", b: "Restart the daemon.", want: false},
		{name: "bulgarian plural negation", a: "Недейте да ползвате --force при push.", b: "Ползвайте --force при push.", want: false},
		{name: "extra identifier", a: "Run the live tests.", b: "Run the live tests with -tags kglive and AO_KNOWLEDGE_GRAPH_URL set.", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameClaim(tt.a, tt.b); got != tt.want {
				t.Fatalf("sameClaim(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
