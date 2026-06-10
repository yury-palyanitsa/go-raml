package com.acronis.raml.lsp

import com.acronis.raml.settings.RamlSettings
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.ide.plugins.PluginManagerCore
import com.intellij.openapi.diagnostic.thisLogger
import com.intellij.openapi.extensions.PluginId
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import java.io.File

/**
 * Describes how to launch the raml-lsp server binary for a project.
 *
 * Binary resolution order:
 *  1. Path configured in Settings > Tools > RAML Language Server
 *  2. Bundled binary: <plugin-dir>/bin/raml-lsp[.exe]
 */
class RamlLspServerDescriptor(project: Project) : ProjectWideLspServerDescriptor(project, "RAML Language Server") {

    override fun isSupportedFile(file: VirtualFile): Boolean =
        file.extension?.lowercase() == "raml"

    override fun createCommandLine(): GeneralCommandLine {
        val binary = resolveBinary()
            ?: error(
                "RAML Language Server binary not found. " +
                "Configure the path in Settings > Tools > RAML Language Server, " +
                "or build it with: go build -trimpath -o bin/raml-lsp ./cmd/raml-lsp/"
            )
        return GeneralCommandLine(binary.absolutePath)
    }

    // -------------------------------------------------------------------------

    private fun resolveBinary(): File? {
        // 1. User-configured path.
        val configured = RamlSettings.getInstance().serverPath.trim()
        if (configured.isNotEmpty()) {
            val f = File(configured)
            if (f.exists() && f.canExecute()) return f
            thisLogger().warn("ramlLsp.serverPath points to a non-existent or non-executable file: $configured")
        }

        // 2. Binary bundled inside the plugin distribution (bin/ directory).
        val pluginId = PluginId.getId("com.acronis.raml")
        val descriptor = PluginManagerCore.getPlugin(pluginId) ?: return null
        val binaryName = if (isWindows()) "raml-lsp.exe" else "raml-lsp"
        val bundled = descriptor.pluginPath.resolve("bin").resolve(binaryName).toFile()
        if (bundled.exists() && bundled.canExecute()) return bundled

        return null
    }

    private fun isWindows(): Boolean =
        System.getProperty("os.name", "").lowercase().contains("win")
}
