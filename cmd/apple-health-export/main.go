// SPDX-License-Identifier: AGPL-3.0-only

// Command apple-health-export creates a verified local Apple Health derivative.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	exp "github.com/soma-intelligence/open-health-sync/connectors/applehealth/export"
)

func main() {
	output := flag.String("output", "", "new private output directory")
	deadline := flag.Duration("timeout", 20*time.Minute, "conversion deadline")
	flag.Parse()
	if *output == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: apple-health-export -output NEW_DIRECTORY export.zip")
		os.Exit(2)
	}
	source, err := exp.Open(flag.Arg(0))
	if err == nil {
		defer source.Close()
		ctx, cancel := context.WithTimeout(context.Background(), *deadline)
		defer cancel()
		var result exp.ArchiveManifest
		result, err = exp.WriteArchive(ctx, source, *output)
		if err == nil {
			fmt.Printf("complete: %d unique rows, %d source files; local output only\n", result.Stats.Lines, len(result.Files))
			return
		}
	}
	fmt.Fprintln(os.Stderr, "conversion failed; output is incomplete and must not be imported")
	os.Exit(1)
}
