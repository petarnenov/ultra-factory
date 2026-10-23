package knowledgegraph

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatSection(t *testing.T) {
	tests := []struct {
		name    string
		items   []Item
		want    []string
		notWant []string
	}{
		{
			name:    "no items still asks to record",
			items:   nil,
			want:    []string{"## Knowledge Graph", SectionTrustBoundary, "No earlier task in this project matched this one yet.", "### Recording lessons", RecordInstruction},
			notWant: []string{"### Earlier attempts", "### Lessons from", "### Similar past"},
		},
		{
			name: "groups in fixed order",
			items: []Item{
				{Section: "task", Title: "Similar", Outcome: "merged", Text: "done: shipped"},
				{Section: "lesson", Kind: "pitfall", Title: "Src task", Outcome: "failed", Text: "Run go vet"},
				{Section: "attempt", Title: "First try", Lessons: []string{"use -race"}},
			},
			want: []string{
				"### Earlier attempts on this issue\n\n- [unknown] First try\n  - lesson: use -race",
				"### Lessons from similar tasks\n\n- [pitfall] (from failed task \"Src task\") Run go vet",
				"### Similar past tasks\n\n- [merged] Similar — done: shipped",
			},
			notWant: []string{"No earlier task"},
		},
		{
			name:  "attempt with PRs and branch",
			items: []Item{{Section: "attempt", Title: "Try", PRs: []string{"https://x/pr/1", "https://x/pr/2"}, Branch: "ao/s1-fix", Lessons: []string{"l"}}},
			want:  []string{"- [unknown] Try\n  PR: https://x/pr/1, https://x/pr/2 · branch: ao/s1-fix\n  - lesson: l"},
		},
		{
			name:    "attempt with only a branch",
			items:   []Item{{Section: "attempt", Title: "Try", Branch: "ao/s1-fix"}},
			want:    []string{"- [unknown] Try\n  branch: ao/s1-fix"},
			notWant: []string{"PR:"},
		},
		{
			name:    "attempt with only PRs",
			items:   []Item{{Section: "attempt", Title: "Try", PRs: []string{"https://x/pr/1"}}},
			want:    []string{"- [unknown] Try\n  PR: https://x/pr/1"},
			notWant: []string{"branch:"},
		},
		{
			name:    "attempt without PRs or branch",
			items:   []Item{{Section: "attempt", Title: "Try"}},
			want:    []string{"- [unknown] Try"},
			notWant: []string{"PR:", "branch:"},
		},
		{
			name:  "lesson without kind or source title",
			items: []Item{{Section: "lesson", Text: "plain"}},
			want:  []string{"- [tip] plain"},
		},
		{
			name:    "unknown sections are ignored",
			items:   []Item{{Section: "other", Title: "x"}},
			want:    []string{"No earlier task"},
			notWant: []string{"- [unknown] x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSection(tt.items)

			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Fatalf("FormatSection() missing %q in:\n%s", w, got)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Fatalf("FormatSection() unexpectedly has %q in:\n%s", nw, got)
				}
			}
		})
	}
}

func TestFormatSectionOrdersGroups(t *testing.T) {
	got := FormatSection([]Item{
		{Section: "task", Title: "t"}, {Section: "lesson", Text: "l"}, {Section: "attempt", Title: "a"},
	})

	a := strings.Index(got, "### Earlier attempts")
	l := strings.Index(got, "### Lessons from")
	s := strings.Index(got, "### Similar past")
	r := strings.Index(got, "### Recording lessons")
	if a >= l || l >= s || s >= r {
		t.Fatalf("group order wrong: attempt=%d lesson=%d task=%d record=%d", a, l, s, r)
	}
}

func TestFormatSectionClipsLongText(t *testing.T) {
	long := strings.Repeat("é", maxItemText+50)

	got := FormatSection([]Item{{Section: "lesson", Text: long, Title: strings.Repeat("t", 100), Outcome: "merged"}})

	if !strings.Contains(got, strings.Repeat("é", maxItemText)+"…") {
		t.Fatalf("lesson text not clipped to %d runes", maxItemText)
	}
	if strings.Contains(got, strings.Repeat("é", maxItemText+1)) {
		t.Fatalf("lesson text longer than %d runes", maxItemText)
	}
	if !strings.Contains(got, `"`+strings.Repeat("t", 80)+`…"`) {
		t.Fatalf("source title not clipped to 80 runes")
	}
}

func TestFormatSectionStopsAfterSectionBudget(t *testing.T) {
	var items []Item
	for i := 0; i < 40; i++ {
		items = append(items, Item{Section: "attempt", Title: "a", Text: strings.Repeat("x", maxItemText)})
	}
	items = append(items, Item{Section: "lesson", Text: "never shown"})

	got := FormatSection(items)

	if strings.Contains(got, "never shown") {
		t.Fatalf("groups after the budget should be dropped")
	}
	if !strings.HasSuffix(got, RecordInstruction) {
		t.Fatalf("record instruction must close the section")
	}
}

func TestClip(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "collapses whitespace", in: " a \n\t b ", n: 10, want: "a b"},
		{name: "exact length kept", in: "abc", n: 3, want: "abc"},
		{name: "runes clipped", in: "ääää", n: 2, want: "ää…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clip(tt.in, tt.n)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("clip() = %q, want %q", got, tt.want)
			}
		})
	}
}
