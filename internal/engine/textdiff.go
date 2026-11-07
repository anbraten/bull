package engine

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	diffContext = 3 // unchanged lines shown around each change
	// Larger inputs skip the LCS table and show the differing middle section
	// as one block of removals followed by additions.
	maxLCSCells = 4_000_000
	// Content of newly created files is only previewed up to this many lines.
	maxCreatePreview = 30
)

type opKind byte

const (
	opEqual  opKind = ' '
	opDelete opKind = '-'
	opInsert opKind = '+'
)

type lineOp struct {
	kind opKind
	text string
}

// splitLines splits s into lines without their trailing newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func isBinary(s string) bool {
	return strings.IndexByte(s, 0) >= 0 || !utf8.ValidString(s)
}

// lineDiff returns the edit script turning a into b.
func lineDiff(a, b []string) []lineOp {
	// Trim the common prefix and suffix; most config edits are small.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}

	var ops []lineOp
	for _, l := range a[:pre] {
		ops = append(ops, lineOp{opEqual, l})
	}
	ops = append(ops, lcsDiff(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, lineOp{opEqual, l})
	}
	return ops
}

func lcsDiff(a, b []string) []lineOp {
	var ops []lineOp
	if len(a)*len(b) > maxLCSCells {
		for _, l := range a {
			ops = append(ops, lineOp{opDelete, l})
		}
		for _, l := range b {
			ops = append(ops, lineOp{opInsert, l})
		}
		return ops
	}

	// lcs[i][j] = length of the LCS of a[i:] and b[j:]
	w := len(b) + 1
	lcs := make([]int, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, lineOp{opEqual, a[i]})
			i++
			j++
		case lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			ops = append(ops, lineOp{opDelete, a[i]})
			i++
		default:
			ops = append(ops, lineOp{opInsert, b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, lineOp{opDelete, a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, lineOp{opInsert, b[j]})
	}
	return ops
}

type hunk struct {
	aStart, aLen, bStart, bLen int // 1-based starts, as in unified diff headers
	ops                        []lineOp
}

func (h hunk) header() string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.aStart, h.aLen, h.bStart, h.bLen)
}

// hunks groups an edit script into unified-diff hunks with diffContext lines
// of context around each change.
func hunks(ops []lineOp) []hunk {
	var changes []int
	for i, op := range ops {
		if op.kind != opEqual {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return nil
	}

	// Line numbers (0-based) in a and b before each op.
	aLine := make([]int, len(ops)+1)
	bLine := make([]int, len(ops)+1)
	for i, op := range ops {
		aLine[i+1], bLine[i+1] = aLine[i], bLine[i]
		if op.kind != opInsert {
			aLine[i+1]++
		}
		if op.kind != opDelete {
			bLine[i+1]++
		}
	}

	var out []hunk
	start := max(changes[0]-diffContext, 0)
	end := min(changes[0]+diffContext+1, len(ops))
	flush := func() {
		h := hunk{
			aStart: aLine[start] + 1, aLen: aLine[end] - aLine[start],
			bStart: bLine[start] + 1, bLen: bLine[end] - bLine[start],
			ops: ops[start:end],
		}
		// Unified diff convention: an empty range starts at the line before.
		if h.aLen == 0 {
			h.aStart--
		}
		if h.bLen == 0 {
			h.bStart--
		}
		out = append(out, h)
	}
	for _, c := range changes[1:] {
		if c-diffContext <= end {
			end = min(c+diffContext+1, len(ops))
			continue
		}
		flush()
		start, end = c-diffContext, min(c+diffContext+1, len(ops))
	}
	flush()
	return out
}

// redactSecrets replaces every occurrence of a secret value in s.
// Longer secrets are replaced first so one that contains another is fully hidden.
func redactSecrets(s string, secrets map[string]string) string {
	vals := make([]string, 0, len(secrets))
	for _, v := range secrets {
		if v != "" {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	return s
}

// textDiffLines renders a text diff as a short summary plus indented body
// lines (already colored), for display under a diff tree entry. Unless full
// is set, new files are previewed only up to maxCreatePreview lines.
func textDiffLines(d Diff, change ChangeType, secrets map[string]string, full bool) (summary string, body []string) {
	if isBinary(d.Before) || isBinary(d.After) {
		if change == ChangeCreate {
			return fmt.Sprintf("binary, %d bytes", len(d.After)), nil
		}
		return fmt.Sprintf("binary, %d → %d bytes", len(d.Before), len(d.After)), nil
	}

	after := splitLines(d.After)

	if change == ChangeCreate {
		n := len(after)
		summary = green(fmt.Sprintf("+%d line%s", n, plural(n)))
		for i, l := range after {
			if !full && i == maxCreatePreview {
				body = append(body, faint(fmt.Sprintf("… %d more line%s (--full-diff to show)", n-i, plural(n-i))))
				break
			}
			body = append(body, green("+"+redactSecrets(l, secrets)))
		}
		return summary, body
	}

	ops := lineDiff(splitLines(d.Before), after)
	adds, dels := 0, 0
	for _, op := range ops {
		switch op.kind {
		case opInsert:
			adds++
		case opDelete:
			dels++
		}
	}

	hs := hunks(ops)
	if len(hs) == 0 {
		// Hashes differ but no line does: only the trailing newline changed.
		return faint("trailing newline changed"), nil
	}

	summary = green(fmt.Sprintf("+%d", adds)) + " " + red(fmt.Sprintf("-%d", dels))
	for _, h := range hs {
		body = append(body, cyan(h.header()))
		for _, op := range h.ops {
			line := string(op.kind) + redactSecrets(op.text, secrets)
			switch op.kind {
			case opInsert:
				line = green(line)
			case opDelete:
				line = red(line)
			default:
				line = faint(line)
			}
			body = append(body, line)
		}
	}
	return summary, body
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
