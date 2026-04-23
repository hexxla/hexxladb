package main

import (
	"fmt"
	"strings"
)

// ELI5 ("explain like I'm 5") section headers: a short metaphor + optional extra lines.
func eli5Banner(title, metaphor string, more ...string) {
	const bar = "================================================================================"
	fmt.Println()
	fmt.Println(bar)
	fmt.Printf(" ELI5 — %s\n", title)
	fmt.Println(bar)
	fmt.Println(wrapIndent(metaphor, 2))
	for _, m := range more {
		fmt.Println(wrapIndent(m, 2))
	}
	fmt.Println(bar)
	fmt.Println()
}

func wrapIndent(s string, indent int) string {
	prefix := strings.Repeat(" ", indent)
	var b strings.Builder
	first := true
	for line := range strings.SplitSeq(s, "\n") {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(prefix)
		b.WriteString(strings.TrimRight(line, " \t"))
	}
	return b.String()
}
