package main

import (
	"fmt"
	"os"

	"github.com/enough/enough/frontend/tui"
)

func main() {
	if err := tui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "enough: %v\n", err)
		os.Exit(1)
	}
}
