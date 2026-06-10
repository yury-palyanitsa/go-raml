package com.acronis.raml.settings

import com.intellij.openapi.options.BoundConfigurable
import com.intellij.openapi.ui.DialogPanel
import com.intellij.ui.dsl.builder.COLUMNS_LARGE
import com.intellij.ui.dsl.builder.bindText
import com.intellij.ui.dsl.builder.columns
import com.intellij.ui.dsl.builder.panel

/** Settings page shown under Settings > Tools > RAML Language Server. */
class RamlSettingsConfigurable : BoundConfigurable("RAML Language Server") {

    private val settings = RamlSettings.getInstance()

    override fun createPanel(): DialogPanel = panel {
        group("Server Binary") {
            row("Server path:") {
                textField()
                    .bindText(settings::serverPath)
                    .columns(COLUMNS_LARGE)
                    .comment(
                        "Absolute path to the <code>raml-lsp</code> binary. " +
                        "Leave empty to use the binary bundled with the plugin.<br>" +
                        "Build from source: " +
                        "<code>go build -trimpath -o bin/raml-lsp ./cmd/raml-lsp/</code>"
                    )
            }
        }
    }
}
