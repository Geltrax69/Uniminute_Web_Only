// Read public catalogue JSON from stdin and emit a reproducible, read-only
// classification/duplicate report. No credentials or writes are needed.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/Geltrax69/Lamazon/backend/classification"
	"os"
)

func main() {
	var products []classification.Input
	if err := json.NewDecoder(os.Stdin).Decode(&products); err != nil {
		fmt.Fprintln(os.Stderr, "read catalogue:", err)
		os.Exit(1)
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	if err := out.Encode(classification.Audit(products)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
