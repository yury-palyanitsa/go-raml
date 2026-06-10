package com.acronis.raml.lsp

import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.LspServerSupportProvider.LspServerStarter

/** Activates the RAML Language Server whenever a .raml file is opened. */
class RamlLspServerSupportProvider : LspServerSupportProvider {

    override fun fileOpened(project: Project, file: VirtualFile, serverStarter: LspServerStarter) {
        if (file.extension?.lowercase() == "raml") {
            serverStarter.ensureServerStarted(RamlLspServerDescriptor(project))
        }
    }
}
