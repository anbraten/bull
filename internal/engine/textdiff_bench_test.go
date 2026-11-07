package engine

import (
	"fmt"
	"testing"
)

func benchLines(n int, mutate func(i int) bool) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d: some typical config content here", i)
		if mutate != nil && mutate(i) {
			out[i] += " changed"
		}
	}
	return out
}

func BenchmarkLineDiff(b *testing.B) {
	cases := []struct {
		name   string
		n      int
		mutate func(int) bool
	}{
		{"200 lines, 1 change", 200, func(i int) bool { return i == 100 }},
		{"2000 lines, 1 change", 2000, func(i int) bool { return i == 1000 }},
		{"2000 lines, first+last changed", 2000, func(i int) bool { return i == 0 || i == 1999 }},
		{"2000 lines, every 10th changed", 2000, func(i int) bool { return i%10 == 0 }},
		{"20000 lines, first+last changed (fallback)", 20000, func(i int) bool { return i == 0 || i == 19999 }},
	}
	for _, c := range cases {
		a, bb := benchLines(c.n, nil), benchLines(c.n, c.mutate)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				hunks(lineDiff(a, bb))
			}
		})
	}
}
