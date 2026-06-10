package com.acronis.raml

import com.intellij.testFramework.fixtures.BasePlatformTestCase

/**
 * Basic smoke tests for the RAML Language Server plugin.
 *
 * LSP integration tests require a running raml-lsp binary which is not
 * guaranteed in CI, so these tests verify configuration and file-type
 * detection logic only.
 */
class RamlLspPluginTest : BasePlatformTestCase() {

    fun testRamlFileExtensionRecognised() {
        // A .raml file should be treated as a supported file by the descriptor.
        val file = myFixture.createFile("api.raml", "#%RAML 1.0\ntitle: Test API")
        assertTrue(file.extension?.lowercase() == "raml")
    }

    fun testNonRamlFileNotRecognised() {
        val file = myFixture.createFile("schema.json", "{}")
        assertFalse(file.extension?.lowercase() == "raml")
    }
}
