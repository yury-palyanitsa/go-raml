package com.acronis.raml.settings

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage

/**
 * Application-level persistent settings for the RAML Language Server plugin.
 * Stored in the IDE config directory as raml-lsp.xml.
 */
@State(
    name = "RamlSettings",
    storages = [Storage("raml-lsp.xml")],
)
@Service(Service.Level.APP)
class RamlSettings : PersistentStateComponent<RamlSettings.State> {

    data class State(
        /** Absolute path to the raml-lsp binary. Empty = use the bundled binary. */
        var serverPath: String = "",
    )

    private var _state = State()

    override fun getState(): State = _state

    override fun loadState(state: State) {
        _state = state
    }

    var serverPath: String
        get() = _state.serverPath
        set(value) { _state.serverPath = value }

    companion object {
        fun getInstance(): RamlSettings =
            ApplicationManager.getApplication().getService(RamlSettings::class.java)
    }
}
