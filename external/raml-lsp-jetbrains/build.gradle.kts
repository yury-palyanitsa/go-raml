import org.jetbrains.intellij.platform.gradle.TestFrameworkType
import org.jetbrains.intellij.platform.gradle.tasks.PrepareSandboxTask

plugins {
    id("org.jetbrains.kotlin.jvm")
    id("org.jetbrains.intellij.platform")
}

// ---------------------------------------------------------------------------
// Platform matrix
// Same targets as external/raml-lsp-vscode/scripts/package-platforms.js.
// ---------------------------------------------------------------------------

data class Platform(
    val target: String,
    val goos: String,
    val goarch: String,
    val goarm: String? = null,
    val exe: Boolean = false,
)

val platforms = listOf(
    Platform("win32-x64",    "windows", "amd64", exe = true ),
    Platform("win32-arm64",  "windows", "arm64", exe = true ),
    Platform("linux-x64",    "linux",   "amd64"             ),
    Platform("linux-arm64",  "linux",   "arm64"             ),
    Platform("linux-armhf",  "linux",   "arm",   goarm = "7"),
    Platform("darwin-x64",   "darwin",  "amd64"             ),
    Platform("darwin-arm64", "darwin",  "arm64"             ),
)

/** Returns the vsce-style target string for the current JVM host. */
fun currentPlatformTarget(): String {
    val os   = System.getProperty("os.name", "").lowercase()
    val arch = System.getProperty("os.arch", "").lowercase()
    val osKey = when {
        os.contains("win") -> "win32"
        os.contains("mac") -> "darwin"
        else               -> "linux"
    }
    val archKey = when {
        arch.contains("aarch64") || arch.contains("arm64") -> "arm64"
        arch.contains("arm")                               -> "armhf"
        else                                               -> "x64"
    }
    return "$osKey-$archKey"
}

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

/** Directory that holds the bundled raml-lsp binary. */
val binDir = layout.projectDirectory.dir("bin")

// ---------------------------------------------------------------------------
// Plugin dependencies
// ---------------------------------------------------------------------------

dependencies {
    testImplementation("junit:junit:4.13.2")

    intellijPlatform {
        intellijIdea("2025.2.1")
        testFramework(TestFrameworkType.Platform)
        bundledPlugin("org.jetbrains.plugins.textmate")
    }
}

// ---------------------------------------------------------------------------
// Include bundled binary in the plugin distribution
// ---------------------------------------------------------------------------

/**
 * Copy the contents of bin/ into the plugin sandbox so the binary is
 * available at <plugin-dir>/bin/raml-lsp[.exe] at runtime.
 * The copy is skipped gracefully if bin/ does not exist yet.
 */
tasks.withType<PrepareSandboxTask> {
    if (binDir.asFile.exists()) {
        from(binDir) { into("${rootProject.name}/bin") }
    }
    val tmDir = layout.projectDirectory.dir("textmate/raml")
    if (tmDir.asFile.exists()) {
        from(tmDir) { into("${rootProject.name}/textmate/raml") }
    }
}

// ---------------------------------------------------------------------------
// Go binary tasks
// ---------------------------------------------------------------------------

/**
 * Builds the raml-lsp binary for the current host platform into bin/.
 * For cross-compilation use packagePlatform / packageAllPlatforms.
 */
tasks.register<Exec>("buildGoBinary") {
    description = "Build the raml-lsp Go binary for the current host platform."
    group = "build"
    notCompatibleWithConfigurationCache("References script-level layout/binDir properties")

    val isWindows = System.getProperty("os.name").lowercase().contains("windows")
    val binaryName = if (isWindows) "raml-lsp.exe" else "raml-lsp"
    val outFile = binDir.file(binaryName).asFile

    doFirst { binDir.asFile.mkdirs() }

    commandLine("go", "build", "-trimpath", "-o", outFile.absolutePath, ".")
    workingDir(layout.projectDirectory.dir("../../cmd/raml-lsp"))
}

// ---------------------------------------------------------------------------
// Multi-platform packaging tasks
// ---------------------------------------------------------------------------

val gradlewCmd: List<String> = if (System.getProperty("os.name", "").lowercase().contains("win"))
    listOf("cmd", "/c", "${projectDir}\\gradlew.bat")
else
    listOf("${projectDir}/gradlew")

/** Runs an external command, inheriting stdio, and throws on non-zero exit. */
fun runCmd(args: List<String>, dir: File, extraEnv: Map<String, String> = emptyMap()) {
    val pb = ProcessBuilder(args)
        .directory(dir)
        .inheritIO()
    pb.environment().putAll(extraEnv)
    val exit = pb.start().waitFor()
    check(exit == 0) { "Command failed (exit $exit): ${args.joinToString(" ")}" }
}

/**
 * Cross-compiles raml-lsp for [plat], runs `./gradlew buildPlugin` in a
 * subprocess so prepareSandbox picks up the fresh binary, then renames the
 * output ZIP from  raml-lsp-jetbrains-X.Y.Z.zip
 *                →  raml-lsp-jetbrains-X.Y.Z-<target>.zip
 */
fun buildAndPackage(plat: Platform) {
    val goSrcDir   = layout.projectDirectory.dir("../../cmd/raml-lsp").asFile
    val distDir    = layout.buildDirectory.dir("distributions").get().asFile
    val binaryName = if (plat.exe) "raml-lsp.exe" else "raml-lsp"

    // Remove any stale binary from a previous platform build.
    binDir.asFile.mkdirs()
    binDir.asFile.listFiles()
        ?.filter { it.name.startsWith("raml-lsp") }
        ?.forEach { it.delete() }

    // Cross-compile.
    logger.lifecycle("  go build  GOOS=${plat.goos} GOARCH=${plat.goarch}${if (plat.goarm != null) " GOARM=${plat.goarm}" else ""}")
    val goEnv = buildMap {
        put("GOOS", plat.goos)
        put("GOARCH", plat.goarch)
        put("CGO_ENABLED", "0")
        if (plat.goarm != null) put("GOARM", plat.goarm)
    }
    runCmd(
        listOf("go", "build", "-trimpath", "-o", binDir.file(binaryName).asFile.absolutePath, "."),
        goSrcDir,
        goEnv,
    )

    // Package — sub-invoke Gradle so that this execution's prepareSandbox
    // configuration-cache entry is not reused with the wrong binary.
    logger.lifecycle("  ./gradlew buildPlugin")
    runCmd(gradlewCmd + "buildPlugin", projectDir)

    // Rename the output ZIP to include the platform suffix.
    val zip = distDir.listFiles()
        ?.filter { it.name.endsWith(".zip") && !it.name.contains("-${plat.target}") }
        ?.maxByOrNull { it.lastModified() }
        ?: error("No ZIP found in $distDir after buildPlugin")

    val renamed = File(zip.parent, zip.name.removeSuffix(".zip") + "-${plat.target}.zip")
    zip.renameTo(renamed)
    logger.lifecycle("    ✓ ${renamed.name}")
}

/**
 * Cross-compile raml-lsp and package a plugin ZIP for the current host platform.
 *
 * Usage:
 *   ./gradlew packageCurrentPlatform
 */
tasks.register("packageCurrentPlatform") {
    description = "Build raml-lsp and package the plugin ZIP for the current host platform."
    group = "distribution"
    notCompatibleWithConfigurationCache("Calls buildAndPackage which references script-level properties")

    doLast {
        val target = currentPlatformTarget()
        val plat = platforms.find { it.target == target }
            ?: error("No platform matrix entry for host target '$target'.")
        logger.lifecycle("\n=== $target ===")
        buildAndPackage(plat)
        logger.lifecycle("\nDone.")
    }
}

/**
 * Cross-compile raml-lsp for every supported platform and produce one
 * platform-specific plugin ZIP per target.
 *
 * Usage:
 *   ./gradlew packageAllPlatforms
 *
 * Output: build/distributions/raml-lsp-jetbrains-<version>-<target>.zip
 */
tasks.register("packageAllPlatforms") {
    description = "Build raml-lsp and package the plugin ZIP for every supported platform."
    group = "distribution"
    notCompatibleWithConfigurationCache("Calls buildAndPackage which references script-level properties")

    doLast {
        for (plat in platforms) {
            logger.lifecycle("\n=== ${plat.target} ===")
            buildAndPackage(plat)
        }
        logger.lifecycle("\nDone.")
    }
}
