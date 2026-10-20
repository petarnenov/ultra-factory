package controllers

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// maxFileChangeDetailBytes caps the patch text one file_change carries in a
// page; the rest is at GET .../conversation/activities/{activityId}.
const maxFileChangeDetailBytes = 16 * 1024

// fileChangeContextLines is the context kept around a changed region.
const fileChangeContextLines = 3

// boundActivityDetail cuts a file_change detail to a compact diff and sets
// detailTruncated when it removed anything. Other kinds pass through.
func boundActivityDetail(kind domain.ActivityKind, detail map[string]any) map[string]any {
	if kind != domain.ActivityKindFileChange || detail == nil {
		return detail
	}
	cut := false
	budget := maxFileChangeDetailBytes

	if files, ok := detail["files"].([]any); ok {
		for _, raw := range files {
			file, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			oldText, hasOld := file["oldText"].(string)
			newText, hasNew := file["newText"].(string)
			if hasOld || hasNew {
				// Their stored patch replaces the whole file; a diff is smaller.
				path, _ := file["path"].(string)
				file["patch"] = compactPatch(path, oldText, newText)
				delete(file, "oldText")
				delete(file, "newText")
				cut = true
			}
			if patch, ok := file["patch"].(string); ok {
				if len(patch) > budget {
					patch = truncateUTF8(patch, budget)
					file["patchTruncated"] = true
					cut = true
				}
				budget -= len(patch)
				file["patch"] = patch
			}
		}
	}
	if content, ok := detail["content"].([]any); ok {
		// Content blocks repeat each diff's full texts.
		for _, raw := range content {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"oldText", "newText"} {
				if _, present := item[key]; present {
					delete(item, key)
					cut = true
				}
			}
		}
	}
	if text, ok := detail["patchOutput"].(string); ok && len(text) > budget {
		detail["patchOutput"] = truncateUTF8(text, max(budget, 0))
		detail["patchOutputTruncated"] = true
		cut = true
	}
	if cut {
		detail["detailTruncated"] = true
	}
	return detail
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// compactPatch renders one unified-diff hunk: the lines between the common
// prefix and suffix plus context. Equal texts yield no patch.
func compactPatch(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines := strings.Split(strings.ReplaceAll(oldText, "\r\n", "\n"), "\n")
	newLines := strings.Split(strings.ReplaceAll(newText, "\r\n", "\n"), "\n")
	if oldText == "" {
		oldLines = nil
	}
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	start := max(prefix-fileChangeContextLines, 0)
	endOld := min(len(oldLines)-suffix+fileChangeContextLines, len(oldLines))
	endNew := min(len(newLines)-suffix+fileChangeContextLines, len(newLines))

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@", path, path,
		hunkStart(start, endOld), endOld-start, hunkStart(start, endNew), endNew-start)
	for _, line := range oldLines[start:prefix] {
		b.WriteString("\n " + line)
	}
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		b.WriteString("\n-" + line)
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		b.WriteString("\n+" + line)
	}
	for _, line := range oldLines[len(oldLines)-suffix : endOld] {
		b.WriteString("\n " + line)
	}
	return b.String()
}

// hunkStart is a hunk's 1-based first line, or 0 for an empty side.
func hunkStart(start, end int) int {
	if end == start {
		return 0
	}
	return start + 1
}
