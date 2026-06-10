package com.acronis.raml.actions

import com.acronis.raml.lsp.RamlLspServerSupportProvider
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.platform.lsp.api.LspServerManager

/** Stops and restarts the RAML Language Server for the current project. */
class RestartRamlLspAction : AnAction() {

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        LspServerManager.getInstance(project)
            .stopAndRestartIfNeeded(RamlLspServerSupportProvider::class.java)
    }

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabled = e.project != null
    }
}
