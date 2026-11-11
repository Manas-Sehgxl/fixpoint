package snapshot

import (
	"fmt"
	"strings"
)

type opKind byte

const (
	opEqual opKind = ' '
	opDel   opKind = '-'
	opIns   opKind = '+'
)

type op struct {
	kind opKind
	line string // includes its trailing "\n" unless it is a last line without one
}

// splitLines splits text into lines that keep their "\n", so a last line
// without a newline never compares equal to the same line with one.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// maxTrace bounds the memory of the Myers trace; past it the remaining
// middle is reported as one replacement, which is still a correct diff.
const maxTrace = 8_000_000

// lineOps returns an edit script turning a into b.
func lineOps(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []op
	for _, l := range a[:pre] {
		ops = append(ops, op{opEqual, l})
	}
	ops = append(ops, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, op{opEqual, l})
	}
	return ops
}

// myers is the classic O(ND) shortest edit script with a stored trace.
func myers(a, b []string) []op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	max := n + m
	off := max + 1
	v := make([]int, 2*max+3)
	var trace [][]int
	found := false
	for d := 0; d <= max && !found; d++ {
		if (d+1)*len(v) > maxTrace {
			return replaceAll(a, b)
		}
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	var rev []op
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vd := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vd[off+k-1] < vd[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vd[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, op{opEqual, a[x-1]})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, op{opIns, b[y-1]})
				y--
			} else {
				rev = append(rev, op{opDel, a[x-1]})
				x--
			}
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func replaceAll(a, b []string) []op {
	ops := make([]op, 0, len(a)+len(b))
	for _, l := range a {
		ops = append(ops, op{opDel, l})
	}
	for _, l := range b {
		ops = append(ops, op{opIns, l})
	}
	return ops
}

// Context is the number of unchanged lines around each hunk.
const Context = 3

// Unified returns a unified diff of old and new for path, applicable with
// `git apply`, and the counts of added and deleted lines. An empty old or new
// with exists false marks an added or deleted file. It returns "" when the
// texts are equal.
func Unified(path, old, new string, oldExists, newExists bool) (patch string, adds, dels int) {
	ops := lineOps(splitLines(old), splitLines(new))
	var changes []int
	for i, o := range ops {
		if o.kind != opEqual {
			changes = append(changes, i)
			if o.kind == opIns {
				adds++
			} else {
				dels++
			}
		}
	}
	if len(changes) == 0 {
		return "", 0, 0
	}
	var sb strings.Builder
	from, to := "a/"+path, "b/"+path
	if !oldExists {
		from = "/dev/null"
	}
	if !newExists {
		to = "/dev/null"
	}
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", from, to)

	// aAt[i] and bAt[i] are the 0-based line positions before ops[i].
	aAt := make([]int, len(ops)+1)
	bAt := make([]int, len(ops)+1)
	for i, o := range ops {
		aAt[i+1], bAt[i+1] = aAt[i], bAt[i]
		if o.kind != opIns {
			aAt[i+1]++
		}
		if o.kind != opDel {
			bAt[i+1]++
		}
	}
	for h := 0; h < len(changes); {
		start := max(changes[h]-Context, 0)
		end := changes[h]
		for h < len(changes) && changes[h]-end <= 2*Context {
			end = changes[h]
			h++
		}
		end = min(end+Context, len(ops)-1)
		aStart, aCount := aAt[start]+1, aAt[end+1]-aAt[start]
		bStart, bCount := bAt[start]+1, bAt[end+1]-bAt[start]
		if aCount == 0 {
			aStart--
		}
		if bCount == 0 {
			bStart--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, o := range ops[start : end+1] {
			sb.WriteByte(byte(o.kind))
			sb.WriteString(o.line)
			if !strings.HasSuffix(o.line, "\n") {
				sb.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return sb.String(), adds, dels
}
