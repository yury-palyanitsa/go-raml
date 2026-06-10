package main

import (
	"flag"
	"log"

	"github.com/acronis/go-raml/cmd/raml-lsp/server"
)

func main() {
	remote := flag.Bool("remote", false,
		"enable resolution of HTTP/HTTPS remote fragments (!include https://...)")
	// -stdio is passed by some LSP clients (e.g. VS Code) to indicate that the
	// server should communicate over stdin/stdout. This server always uses stdio,
	// so the flag is accepted and ignored.
	_ = flag.Bool("stdio", false, "use stdio for LSP communication (always on, accepted for compatibility)")
	flag.Parse()

	s := server.New(server.Options{AllowRemote: *remote})
	if err := s.RunStdio(); err != nil {
		log.Fatalf("raml-lsp: %v", err)
	}
}
