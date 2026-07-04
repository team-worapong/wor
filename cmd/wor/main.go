// Command wor is the entrypoint for the Go rewrite of wor-cli: an
// Infrastructure & Operations tool for managing Node.js/PHP services,
// static sites, host (nginx/apache) configuration, SSL certificates,
// and database backups under one filesystem convention -- now portable
// across Linux, macOS, and Windows.
package main

import (
	"fmt"
	"os"

	"wor/internal/cliapp"
)

func main() {
	app, err := cliapp.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
	os.Exit(app.Run(os.Args[1:]))
}
