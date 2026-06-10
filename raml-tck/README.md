# RAML Test Compliance Kit

This is a copy of [raml-tck](https://github.com/raml-org/raml-tck) repository with customized fixtures.

## Naming convention

- *valid*.raml: valid RAML file expected to be successfully processed
- *invalid*.raml: invalid RAML file with syntax/semantic/spec error(s), expected to be unsuccessfully processed (error or exit code returned)

Note that this repository contains a manifest file that lists all tests in the order their respective tested features appear in the RAML 1.0 Spec.
