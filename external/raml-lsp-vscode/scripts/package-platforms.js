#!/usr/bin/env node
// Builds the raml-lsp Go binary for each platform then packages a
// platform-specific .vsix using @vscode/vsce.
//
// Usage:
//   node scripts/package-platforms.js              # build + package all platforms
//   node scripts/package-platforms.js --current    # build + package host platform only
//   node scripts/package-platforms.js win32-x64 linux-x64  # specific targets

'use strict';

const { execSync } = require('child_process');
const path = require('path');
const fs = require('fs');

// ---------------------------------------------------------------------------
// Platform matrix
// Each entry describes one VS Code / vsce target and the corresponding Go
// cross-compilation settings.
// ---------------------------------------------------------------------------

/** @type {Array<{vsceTarget: string, goos: string, goarch: string, exe: boolean, goarm?: string, cgoDisabled?: boolean}>} */
const PLATFORMS = [
    { vsceTarget: 'win32-x64',    goos: 'windows', goarch: 'amd64', exe: true  },
    { vsceTarget: 'win32-arm64',  goos: 'windows', goarch: 'arm64', exe: true  },
    { vsceTarget: 'linux-x64',    goos: 'linux',   goarch: 'amd64', exe: false },
    { vsceTarget: 'linux-arm64',  goos: 'linux',   goarch: 'arm64', exe: false },
    { vsceTarget: 'linux-armhf',  goos: 'linux',   goarch: 'arm',   exe: false, goarm: '7' },
    { vsceTarget: 'darwin-x64',   goos: 'darwin',  goarch: 'amd64', exe: false },
    { vsceTarget: 'darwin-arm64', goos: 'darwin',  goarch: 'arm64', exe: false },
    { vsceTarget: 'alpine-x64',   goos: 'linux',   goarch: 'amd64', exe: false, cgoDisabled: true },
    { vsceTarget: 'alpine-arm64', goos: 'linux',   goarch: 'arm64', exe: false, cgoDisabled: true },
];

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

/** raml-lsp-vscode/ */
const EXT_DIR  = path.resolve(__dirname, '..');
/** raml-lsp/ (Go source module) */
const GO_SRC   = path.resolve(EXT_DIR, '..', '..', 'cmd', 'raml-lsp');
/** raml-lsp-vscode/bin/ */
const BIN_DIR  = path.join(EXT_DIR, 'bin');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function run(cmd, opts) {
    console.log(`    $ ${cmd}`);
    execSync(cmd, { stdio: 'inherit', ...opts });
}

/**
 * Like run(), but provides 'y\n' on stdin to auto-answer any vsce prompts
 * (e.g. the missing-LICENSE warning in non-interactive CI environments).
 */
function runInteractive(cmd, opts) {
    console.log(`    $ ${cmd}`);
    execSync(cmd, { stdio: ['pipe', 'inherit', 'inherit'], input: 'y\n', ...opts });
}

/**
 * Cross-compile the raml-lsp binary for a given platform into bin/.
 * Removes any leftover binary from a previous run first so the wrong
 * platform binary is never accidentally included in a package.
 */
function buildBinary({ goos, goarch, exe, goarm, cgoDisabled }) {
    fs.mkdirSync(BIN_DIR, { recursive: true });

    // Remove any previous binary regardless of name (including OS backup files).
    // Ignore EPERM on Windows for locked .exe~ swap files.
    for (const entry of fs.readdirSync(BIN_DIR).filter(n => n.startsWith('raml-lsp'))) {
        try { fs.unlinkSync(path.join(BIN_DIR, entry)); } catch { /* ignore locked files */ }
    }

    const binaryName = exe ? 'raml-lsp.exe' : 'raml-lsp';
    const outPath = path.join(BIN_DIR, binaryName);

    const env = {
        ...process.env,
        GOOS: goos,
        GOARCH: goarch,
        CGO_ENABLED: cgoDisabled ? '0' : '0', // always disable CGO for portable binaries
    };
    if (goarm) { env.GOARM = goarm; }

    console.log(`  go build  GOOS=${goos} GOARCH=${goarch}${goarm ? ' GOARM=' + goarm : ''}`);
    run(`go build -trimpath -o "${outPath}" .`, { cwd: GO_SRC, env });
}

/**
 * Run @vscode/vsce to produce a platform-specific .vsix.
 * The output file is named  raml-lsp-vscode-<version>@<target>.vsix
 * and placed in the extension directory.
 */
function packageVsce(vsceTarget) {
    console.log(`  vsce package --target ${vsceTarget}`);
    runInteractive(`npx @vscode/vsce package --target ${vsceTarget} --no-dependencies --allow-missing-repository`, { cwd: EXT_DIR });
}

// ---------------------------------------------------------------------------
// Platform detection for --current flag
// ---------------------------------------------------------------------------

/** Maps Node.js process.platform + process.arch to a vsce target string. */
function currentVsceTarget() {
    const osMap   = { win32: 'win32', linux: 'linux', darwin: 'darwin' };
    const archMap = { x64: 'x64', arm64: 'arm64', arm: 'armhf' };
    const os   = osMap[process.platform];
    const arch = archMap[process.arch];
    if (!os || !arch) {
        throw new Error(`No vsce target mapping for host ${process.platform}/${process.arch}`);
    }
    return `${os}-${arch}`;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

const args = process.argv.slice(2);
const currentOnly = args.includes('--current');
const explicit    = args.filter(a => !a.startsWith('-'));

let selected;
if (currentOnly) {
    const t = currentVsceTarget();
    selected = PLATFORMS.filter(p => p.vsceTarget === t);
    if (!selected.length) {
        console.error(`No configured platform entry for host target "${t}".`);
        process.exit(1);
    }
} else if (explicit.length > 0) {
    selected = PLATFORMS.filter(p => explicit.includes(p.vsceTarget));
    const unknown = explicit.filter(t => !PLATFORMS.some(p => p.vsceTarget === t));
    if (unknown.length) {
        console.error(`Unknown target(s): ${unknown.join(', ')}`);
        console.error(`Valid targets: ${PLATFORMS.map(p => p.vsceTarget).join(', ')}`);
        process.exit(1);
    }
} else {
    selected = PLATFORMS;
}

for (const plat of selected) {
    console.log(`\n=== ${plat.vsceTarget} ===`);
    buildBinary(plat);
    packageVsce(plat.vsceTarget);
    console.log(`    ✓ ${plat.vsceTarget}`);
}

console.log('\nDone.');
