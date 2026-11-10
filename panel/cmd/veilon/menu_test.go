package main

import (
	"bufio"
	"strings"
	"testing"
)

// Enter alone, an unknown key or a section heading's empty key must never
// run anything (a heading has no action) and must not crash the menu.
func TestRunMenuIgnoresEmptyAndHeadings(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	ran := 0
	items := []menuItem{
		{"", "── Section", nil},
		{"1", "Do it", func() error { ran++; return nil }},
	}
	// "", unknown "7" (+Enter to continue), "1" (+Enter to continue), then 0
	stdin = bufio.NewReader(strings.NewReader("\n7\n\n1\n\n0\n"))
	if err := runMenu("", nil, items, "Exit"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("ran %d times, want 1", ran)
	}
}
