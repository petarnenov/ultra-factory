package knowledgegraph

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Embeddings rate "use X" and "do not use X" as close as paraphrases, so
// merged lessons also need the same values and negations in the same order.
func sameClaim(a, b string) bool {
	return slices.Equal(claimMarks(a), claimMarks(b))
}

var negators = map[string]bool{
	"not": true, "never": true, "no": true, "cannot": true, "avoid": true,
	"without": true, "nor": true, "dont": true, "doesnt": true, "didnt": true,
	"isnt": true, "arent": true, "wasnt": true, "werent": true, "wont": true,
	"cant": true, "couldnt": true, "shouldnt": true, "wouldnt": true,
	"mustnt": true, "hasnt": true, "havent": true, "none": true, "nothing": true,
	"neither": true, "nobody": true, "nowhere": true,
	"не": true, "няма": true, "нямат": true, "нямаше": true, "никога": true,
	"без": true, "недей": true, "недейте": true, "нито": true, "нищо": true,
	"никой": true, "никакъв": true, "никаква": true, "никакво": true, "никакви": true,
}

const negationMark = "¬"

var (
	wordOrCode = regexp.MustCompile("`[^`]+`|[^\\s`]+")
	// apostrophes covers typographic forms and a backtick inside a word.
	apostrophes = regexp.MustCompile("(\\pL)[’‘ʼ´′`](\\pL)")
)

// claimMarks lists, in order, the value-carrying tokens (code spans,
// numbers, paths, flags, names like kg-deploy or useQuery) and negations.
func claimMarks(text string) []string {
	var out []string
	for _, tok := range wordOrCode.FindAllString(apostrophes.ReplaceAllString(text, "$1'$2"), -1) {
		if strings.HasPrefix(tok, "`") {
			out = append(out, strings.Trim(tok, "`"))
			continue
		}
		tok = strings.TrimRight(strings.TrimLeft(tok, "\"'("), "\"'),.;:!?")
		if isLiteral(tok) {
			out = append(out, strings.ToLower(tok))
			continue
		}
		if negates(tok) {
			out = append(out, negationMark)
		}
	}
	return out
}

// negates sees through Markdown and quotes: **not**, “never”, „не“.
func negates(tok string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(tok), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	}) {
		w = strings.Trim(w, "'")
		if negators[w] || strings.HasSuffix(w, "n't") {
			return true
		}
	}
	return false
}

func isLiteral(tok string) bool {
	if strings.ContainsAny(tok, "0123456789_=/.#-") {
		return true
	}
	upper := 0
	runes := []rune(tok)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			upper++
		}
		if i > 0 && unicode.IsLower(runes[i-1]) && unicode.IsUpper(r) {
			return true
		}
	}
	return upper >= 2
}
