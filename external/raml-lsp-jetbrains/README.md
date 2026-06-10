# RAML Language Server — JetBrains Plugin

JetBrains IDE plugin that provides RAML 1.0 language support via the
[go-raml](https://github.com/acronis/go-raml) Language Server Protocol (LSP) server.

## Features

Powered by `raml-lsp`, the plugin surfaces the following LSP capabilities:

- **Diagnostics** — parse and validation errors shown inline
- **Hover** — documentation for the element under the cursor
- **Go to Definition** — navigate to type, trait, and resource-type declarations
- **Find References** — list all usages of a declaration
- **Document Symbols** — structure view / breadcrumbs

## Requirements

- IntelliJ IDEA 2025.2.1 or later
- JDK 21 (Gradle build)
- Go 1.22+ (building the server binary from source)

## Installation

Install from the JetBrains Marketplace, or manually via
**Settings ▸ Plugins ▸ ⚙ ▸ Install Plugin from Disk** using a ZIP from the
[Releases](https://github.com/acronis/go-raml/releases) page.

The plugin bundles a `raml-lsp` binary for your platform. No separate installation
of the server is required.

## Configuration

**Settings ▸ Tools ▸ RAML Language Server**

| Setting | Default | Description |
|---------|---------|-------------|
| Server path | _(empty)_ | Absolute path to a custom `raml-lsp` binary. Leave empty to use the binary bundled with the plugin. |

## Actions

| Action | Location | Description |
|--------|----------|-------------|
| Restart RAML Language Server | **Tools** menu | Stops and restarts the running LSP server for the current project. |

## Development

### Run in a sandboxed IDE

```sh
# Build the raml-lsp binary for the current host platform
./gradlew buildGoBinary

# Launch a sandboxed IntelliJ instance with the plugin loaded
./gradlew runIde
```

### Build the plugin ZIP

```sh
# Bundle binary + package plugin for the current platform
./gradlew buildGoBinary buildPlugin
# Output: build/distributions/raml-lsp-jetbrains-<version>.zip
```

## Packaging for multiple platforms

Multi-platform packaging is handled entirely by Gradle — no Node.js required.
Each invocation cross-compiles `raml-lsp` with the appropriate `GOOS`/`GOARCH`,
calls `./gradlew buildPlugin` in a subprocess so `prepareSandbox` picks up the
fresh binary, then renames the output ZIP with the platform suffix.

```sh
# Current host only
./gradlew packageCurrentPlatform

# All platforms
./gradlew packageAllPlatforms
```

Available targets: `win32-x64`, `win32-arm64`, `linux-x64`, `linux-arm64`,
`linux-armhf`, `darwin-x64`, `darwin-arm64`.

Output ZIPs are written to `build/distributions/`
as `raml-lsp-jetbrains-<version>-<target>.zip`.

## Project structure

```
raml-lsp-jetbrains/
├── src/
│   ├── main/
│   │   ├── kotlin/com/acronis/raml/
│   │   │   ├── RamlBundle.kt         # Message bundle helper
│   │   │   ├── actions/
│   │   │   │   └── RestartRamlLspAction.kt
│   │   │   ├── lsp/
│   │   │   │   ├── RamlLspServerDescriptor.kt      # Binary resolution + command line
│   │   │   │   └── RamlLspServerSupportProvider.kt # Activates on .raml file open
│   │   │   └── settings/
│   │   │       ├── RamlSettings.kt              # Persistent state
│   │   │       └── RamlSettingsConfigurable.kt  # Settings UI
│   │   └── resources/
│   │       ├── messages/RamlBundle.properties
│   │       └── META-INF/plugin.xml
│   └── test/kotlin/com/acronis/raml/
│       └── RamlLspPluginTest.kt
├── build.gradle.kts   # Platform matrix + all packaging tasks
├── gradle.properties
└── settings.gradle.kts
```

## License

MIT — see [LICENSE](./LICENSE).
