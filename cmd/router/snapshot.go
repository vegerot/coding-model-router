package main

import (
	"context"
	"fmt"
	"io"
)

// cmdSnapshot is fully implemented in M1.5. This stub keeps M0 compiling.
func cmdSnapshot(_ context.Context, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "router snapshot: not implemented yet")
	return 2
}
