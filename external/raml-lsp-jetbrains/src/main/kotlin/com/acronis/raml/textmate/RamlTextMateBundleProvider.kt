package com.acronis.raml.textmate

import com.intellij.ide.plugins.PluginManagerCore
import com.intellij.openapi.extensions.PluginId
import org.jetbrains.plugins.textmate.api.TextMateBundleProvider

internal class RamlTextMateBundleProvider : TextMateBundleProvider {
    override fun getBundles(): List<TextMateBundleProvider.PluginBundle> {
        val pluginPath = PluginManagerCore.getPlugin(PluginId.getId("com.acronis.raml"))
            ?.pluginPath
            ?: return emptyList()

        val bundlePath = pluginPath.resolve("textmate/raml")
        if (!bundlePath.toFile().isDirectory) return emptyList()

        return listOf(TextMateBundleProvider.PluginBundle("RAML", bundlePath))
    }
}
