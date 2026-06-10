# Directory containing the Makefile.
export PATH := $(GOBIN):$(PATH)

MIN_COVERAGE = 70

.PHONY: all
all: lint cover

.PHONY: lint
lint:
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run ./...

.PHONY: test
test-unit:
	@go test ./...

.PHONY: cover
test-cover:
	@pkgs=$$(go list ./... | grep -v /Store/) \
	go test -coverprofile=cover.out.tmp $${pkgs} -coverpkg=$${pkgs}  \
	&& cat cover.out.tmp | grep -v "rdtparser_base_visitor.go" > cover.out \
	&& rm cover.out.tmp \
	&& go tool cover -func=cover.out | grep total | awk '{print substr($$3, 1, length($$3)-1)}' | \
	awk '{if ($$1 < $(MIN_COVERAGE)) {print "Coverage is below $(MIN_COVERAGE)%!" ; exit 1}}' \
	&& go tool cover -html=cover.out -o cover.html

.PHONY: test
test: test-cover

.PHONY: build
build: go-build

.PHONY: go-build
go-build:
	@echo "Building raml"
	@cd cmd/raml && go build -o ../../.build/raml \
	&& echo "Build successful in .build/raml"

.PHONY: go
go:
	@# Run go command with the specified arguments
	@# examples:
	@# 		make go a="mod tidy"
	@# 		make go a="mod vendor"
	@go $(a)

.PHONY: install
install:
	@echo "Installing raml"
	@cd cmd/raml && go install . \
	&& echo "Installed to $(GOPATH)/bin/raml"

# ---------------------------------------------------------------------------
# CLI utility (cmd/raml)
# ---------------------------------------------------------------------------

.PHONY: build-cli
build-cli: go-build

.PHONY: lint-cli
lint-cli:
	@cd cmd/raml && go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run --timeout=5m -v ./...

# ---------------------------------------------------------------------------
# LSP server (cmd/raml-lsp)
# ---------------------------------------------------------------------------

.PHONY: build-lsp
build-lsp:
	@echo "Building raml-lsp"
	@cd cmd/raml-lsp && go build -o ../../.build/raml-lsp \
	&& echo "Build successful in .build/raml-lsp"

.PHONY: test-lsp
test-lsp:
	@cd cmd/raml-lsp && go test ./...

.PHONY: lint-lsp
lint-lsp:
	@cd cmd/raml-lsp && go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run --timeout=5m -v ./...

# ---------------------------------------------------------------------------
# External plugins
# ---------------------------------------------------------------------------

.PHONY: build-plugin-vscode
build-plugin-vscode:
	@echo "Building VS Code plugin"
	@cd external/raml-lsp-vscode && npm ci && npm run webpack:prod

.PHONY: build-plugin-jetbrains
build-plugin-jetbrains:
	@echo "Building JetBrains plugin"
	@cd external/raml-lsp-jetbrains && ./gradlew buildPlugin
