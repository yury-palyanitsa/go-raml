[![Go Report Card](https://goreportcard.com/badge/gojp/goreportcard)](https://goreportcard.com/report/gojp/goreportcard)
<!---[![Go Coverage](https://github.com/acronis/go-raml/wiki/coverage.svg)](https://raw.githack.com/wiki/acronis/go-raml/coverage.html)--->

# RAML 1.0 parser for Go

> [!WARNING]
> The parser is in active development. See supported features in the **Supported features of RAML 1.0 specification**
> section.

This is an implementation of RAML parser for Go according to
[the official RAML 1.0 specification](https://github.com/raml-org/raml-spec/blob/master/versions/raml-10/raml-10.md/).

This package aims to achieve the following:

1. Provide a compliant RAML 1.0 parser.
1. Provide an easy-to-use interface for parsing and validating RAML/data type definitions.
1. Provide optimal performance and memory efficiency for API/data type definitions of any size.
1. Provide additional, but not limited to, features built on top of parser, such as:
    1. Middlewares for popular HTTP frameworks that validates the requests/responses according to API definition.
    1. An HTTP gateway that validates requests/responses according to API definition.
    1. A language server built according to Language Server Protocol (LSP).
    1. A linter that may provide additional validations or style enforcement.
    1. Conversion of parsed structure into JSON Schema and back to RAML.

## Why RAML?

RAML is a powerful modelling language that encourages a design-first approach to API definitions by offering
modularity and flexibility. As well as the powerful type system with inheritance support that it offers,
it also allows developers to easily define complex data type schemas by leveraging
both an inheritance mechanism (by referencing a parent type with common properties) and modularity (by referencing common data type fragments).

RAML uses YAML as a markup language, which also contributes to readability, especially in complex definitions.

### RAML vs OpenAPI

While both specifications provide a way to define endpoints, request and responses, and security schemes,
RAML additionally provides ways to reuse their content. For example:

* By specifying traits to apply individual reusable properties to specific endpoints or collections.
* By specifying resource types to apply a common set of available actions or collections.
* By utilizing type inheritance when building complex types.

OpenAPI also allows breaking down the specification into individual and reusable components and include them
in the document. However, these components only deduplicate the content and cannot be modified. In RAML, on the other hand,
it is possible to reuse a component AND modify it, thus reducing the boilerplate.
The following simple example demonstrates how common resource types can be used to build a uniform CRUD API:

```yaml
#%RAML 1.0

title: Test API
version: v1
baseUri: /api/{version}
mediaType: application/json

resourceTypes:
  BatchCollection:
    get:
      responses:
        200:
          body:
            type: <<entityType>>[]
    post:
      body:
        type: <<entityType>>
      responses:
        201:
          body:
            type: <<entityType>>
    delete:
      responses:
        204:
  ItemCollection:
    get:
      responses:
        200:
          body:
            type: <<entityType>>
    put:
      body:
        type: <<entityType>>
      responses:
        204:
    delete:
      responses:
        204:

types:
  User:
    properties:
      id: integer
      name: string
      email: string
      age: integer

  Organization:
    properties:
      id: integer
      name: string
      address: string

/users:
  type:
    BatchCollection:
      entityType: User
  /{user_id}:
    type:
      ItemCollection:
        entityType: User

/organizations:
  type:
    BatchCollection:
      entityType: Organization
  /{organization_id}:
    type:
      ItemCollection:
        entityType: Organization
```

Additionally, developers may use typed annotations to introduce custom behavior, contract validation, assist code generation,
auto-tests, etc. Compared to OpenAPI where extensions are untyped, this allows the developers to describe the extension
right in the document.

### RAML data type vs JSON Schema

For comparison, an example of a simple data type schema in RAML would be the following:

```yaml
#%RAML 1.0 Library

types:
  CommonType:
    additionalProperties: false
    properties:
      id: string
      content: object
    example:
      id: sample.message
      content: {}

  DerivedType:
    type: CommonType # inherits from "CommonType"
    properties:
      content:
        properties:
          value: integer
    example:
      id: "metrics.message"
      content:
        value: 0
```

Where `DerivedType` would translate into the following JSON Schema using Draft-7 specification due to the lack
of the inheritance support:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$ref": "#/definitions/DerivedType",
  "definitions": {
    "DerivedType": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "id": {
          "type": "string"
        },
        "content": {
          "type": "object",
          "properties": {
            "value": {
              "type": "integer"
            }
          },
          "required": ["value"]
        }
      },
      "required": ["id", "content"]
    }
  }
}
```

## What you can do with it

### Create API definitions

RAML's primary objective is to provide a specification for maintainable, design-driven API definitions.

By using RAML, you can make a well-structured API definition that is easy to read, maintain
and use.

### Validate data

Given a powerful type definition system, you can declaratively define your data schema and validate
data instances against that schema.

### Define static configuration

On top of the type definition system, RAML allows defining metadata using custom typed annotations.
One of the ways in which a custom annotation can be used by processors is creating a static configuration
that is validated against a specific schema. For example:

```yaml
#%RAML 1.0 Library

types:
  # Define a data type for configuration
  Config:
    additionalProperties: false
    properties:
      host: string
      port: integer

annotationTypes:
  # Define a custom annotation that would be used to create an instance of a type
  ConfigInstance: Config

# Create an instance of a type.
(ConfigInstance):
  host: example.com
  port: 8080
```

### And more

With all these features, more creative cases can be covered than the use cases mentioned here.

## Supported features of RAML 1.0 specification

The following sections are currently implemented. See notes for each point:

- [x] RAML API definitions
  - [x] Resource Types
  - [x] Traits
  - [x] Endpoint definitions
  - [x] Security schemes
  - [x] Documentation
- [x] RAML Data Types
  - [x] Defining Types
  - [x] Type Declarations
  - [x] Built-in Types
    - [x] The "Any" Type
    - [x] Object Type
      - [x] Property Declarations (explicit and pattern properties)
      - [x] Additional Properties
      - [x] Object Type Specialization
      - [x] Using Discriminator
    - [x] Array Type
    - [x] Scalar Types
      - [x] String
      - [x] Number
      - [x] Integer
      - [x] Boolean
      - [x] Date
      - [x] File
      - [x] Nil Type
    - [x] Union Type (mostly supported, lacks enum support)
    - [x] JSON Schema types
    - [x] Recursive types
  - [x] User-defined Facets
  - [x] Determine Default Types
  - [x] Type Expressions
  - [x] Type Inheritance
  - [x] Multiple Inheritance
  - [x] Inline Type Declarations
  - [x] Defining Examples in RAML
    - [x] Multiple Examples
    - [x] Single Example
    - [x] Validation against defined data type
- [ ] Annotations
  - [x] Declaring Annotation Types
  - [ ] Applying Annotations
    - [x] Annotating Scalar-valued Nodes
    - [ ] Annotation Targets
    - [x] Annotating types
- [ ] Modularization
  - [x] Includes
  - [ ] Typed fragments
    - [x] Library
    - [x] NamedExample
    - [x] DataType
    - [x] AnnotationTypeDeclaration
    - [x] DocumentationItem
    - [x] ResourceType
    - [x] Trait
    - [ ] Overlay
    - [ ] Extension
    - [x] SecurityScheme

## Conversion features

- [x] Data type conversion from RAML Data Type to JSON Schema
- [x] Data type conversion from JSON Schema to RAML Data Type
- [x] Conversion to [AMF Graph model](https://github.com/aml-org/amf/blob/develop/documentation/model.md)
- [x] Conversion to OpenAPI 3.0 (API and data type)

## Comparison with existing libraries

### Table of libraries

| Parser                                                         | RAML 1.0 support      | Language         |
|----------------------------------------------------------------|-----------------------|------------------|
| [AML Modeling Framework](https://github.com/aml-org/amf)       | Yes (full support)    | Scala/TypeScript |
| [raml-js-parser](https://github.com/raml-org/raml-js-parser-2) | Yes                   | TypeScript       |
| [ramlfications](https://github.com/jdiegodcp/ramlfications)    | No                    | Python           |
| go-raml                                                        | Yes (partial support) | Go               |

### Performance

Complex project (7124 types, 148 libraries)

| Project Type                | Time taken | RAM taken |
|-----------------------------|------------|-----------|
| go-raml                     | ~280ms     | ~48MB     |
| AML Modeling Framework (TS) | ~17s       | ~870MB    |

Simple project (<100 types, 1 library)

| Project Type                | Time taken | RAM taken |
|-----------------------------|------------|-----------|
| go-raml                     | ~4ms       | ~12MB     |
| AML Modeling Framework (TS) | ~2s        | ~100MB    |

## Known Spec Deviations

### Supported JSON Schema Drafts

Supported JSON Schema Drafts are limited to what is supported
by [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema) library.

### XML Schema (XSD) — Not Supported

XSD external types referenced via `!include foo.xsd` are not supported.
RAML allows XSD as an external type for body schemas; users targeting that
feature should pre-convert their XSDs to JSON Schema or RAML data types.

### Numeric type formats are not cross-compatible

The RAML 1.0 spec states that `integer` inherits all facets from `number`, implying that all 8 format values (`int8`, `int16`, `int32`, `int`, `int64`, `long`, `float`, `double`) are valid for both types. This library deliberately rejects that:

- `number` only accepts `float` and `double` — it is a floating-point type backed by `*big.Rat`.
- `integer` only accepts `int8`, `int16`, `int32`, `int`, `int64`, and `long` — it is an integer type backed by `*big.Int`.

## Implementation specifics

### Regexp engine support is limited to Go

The parser does not support ECMA-262 regular expressions. Instead, it uses Go's regular expression engine, which is RE2-based and does not support backreferences or look-around assertions. This means that some regular expressions that are valid in RAML may not be supported by this parser.

### Includes and root path handling

[RAML Includes](https://github.com/raml-org/raml-spec/blob/master/versions/raml-10/raml-10.md#includes)
allow including files by absolute path, relative path, and a URL. According to the specification,
absolute paths should be interpreted as relative to root RAML location. The parser extends
this behavior by providing workspace folder configuration which is, by default, derived from the
parsed file location. This is compliant with the specification and also allows the library users
to flexibly choose how they can work with the root and dependent fragments that may reference each other via
absolute paths.

For security reasons, the parser restricts file access to the workspace folder and disallows access
to files outside of it. When working with dependent files, it's recommended to set an approparite workspace
folder that is common for all dependent fragments.

Command-line tool by default uses the fragment location as a workspace folder, but
it can be overridden by the `-w` flag. Language server uses workspace folders provided by the client
during the server initialization. For library usage, the workspace folder can be set by using `raml.OptWithWorkspaceRoot(string)` option.

### Remote includes

RAML specification allows including remote files via URL. For security reasons, the
parser disallows them by default. For command-line tool and language server, remote includes
can be enabled by the `-r` flag. For library usage, remote includes can be enabled by using
the `raml.OptWithHTTPClient(*http.Client)` option and either bring their own HTTP client with
the desired configuration or use `raml.NewHTTPClient()` to allow remote includes.

### Reference resolution in typed fragments

All typed fragments (Library, DataType, Trait, ResourceType, etc.) are self-contained.
A fragment may only reference types it declares itself or imports via its own `uses:` map.
It cannot use types from the document that includes it. Non-parameterized trait and
resource type fragments without optional methods are compiled eagerly at parse time; any
unqualified type reference that cannot be resolved within the fragment's own `uses:` is
rejected immediately:

```yaml
# traits/paged.raml
#%RAML 1.0 Trait
responses:
  200:
    body:
      application/json:
        type: PagedResult        # ✗ error: PagedResult not declared in this fragment's uses:
```

All external type dependencies must be imported in the fragment's own `uses:` and
referenced with the qualified (`alias.TypeName`) form:

```yaml
# traits/paged.raml
#%RAML 1.0 Trait
uses:
  models: ../models.raml
responses:
  200:
    body:
      application/json:
        type: models.PagedResult # ✓
```

Inline definitions (declared directly in the root API/library under `traits:`, `resourceTypes:`,
`types:`, etc.) follow the opposite rule: they resolve types against the root API's own
`types:` namespace. Both unqualified and qualified (`lib.Foo`) names work; forward
references are resolved at unwrap time.

### Traits and resource types: template resolution

Traits and resource types are templates: they declare `<<parameters>>` that are filled in
at the point of use. This affects how type names inside a template are resolved.

#### Type parameters

A type name passed as a `<<parameter>>` value is resolved against the **calling
document's** type namespace, not the template's. This applies equally to traits and
resource types. The calling document must import any type library it references in
parameter values; the template itself needs no corresponding `uses:` entry.

`traits/paged.raml`:
```yaml
#%RAML 1.0 Trait
responses:
  200:
    body:
      application/json:
        type: <<responseType>>
```

`api.raml`:
```yaml
#%RAML 1.0
title: Example API
uses:
  types: types.raml
types:
  PagedResult:
    properties:
      items: any[]
      total: integer
traits:
  paged: !include traits/paged.raml
/items:
  get:
    is:
      - paged:
          responseType: types.PagedResult   # resolved from api.raml's uses:
```

The resolved `GET /items` operation:
```yaml
/items:
  get:
    responses:
      200:
        body:
          application/json:
            type: types.PagedResult   # resolved from api.raml's uses:
```

The same rule applies to resource type parameters:

`resourceTypes/collection.raml`:
```yaml
#%RAML 1.0 ResourceType
get:
  responses:
    200:
      body:
        application/json:
          type: <<itemType>>
```

`api.raml`:
```yaml
#%RAML 1.0
title: Example API
uses:
  types: types.raml    # api.raml imports the library, not the RT fragment
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type:
    collection:
      itemType: types.Item   # resolved from api.raml's uses:
```

The resolved `GET /items` operation:
```yaml
/items:
  get:
    responses:
      200:
        body:
          application/json:
            type: types.Item   # resolved from api.raml's uses:
```

#### Trait and resource type name resolution

Trait names in `is:` entries follow **lexical scoping**: a name resolves against the
namespace of the document in which the `is:` entry is physically written.

1. Names written in the **root API** resolve against the root API's `traits:` (and its
   `uses:` for qualified `lib.traitName` forms).
2. Names written **inside a fragment** (e.g. a ResourceType or Trait fragment) resolve
   against that fragment's own `traits:`/`uses:` ONLY. There is no fallback to the
   including API's trait namespace.

Because a fragment has no implicit access to the root API's traits, an unqualified name
written inside a fragment is a dangling reference and is rejected. To reference an API- or
library-defined trait from within a fragment, import it via the fragment's own `uses:` and
use the qualified form — this keeps the fragment self-contained.

`resourceTypes/traits.raml`:

```yaml
#%RAML 1.0 Library
traits:
  paged:
    queryParameters:
      page: integer
      pageSize: integer
```

`resourceTypes/collection.raml`:

```yaml
#%RAML 1.0 ResourceType
uses:
  traitsLib: traits.raml
get:
  is: [traitsLib.paged]   # qualified via the fragment's own uses:
  responses:
    200:
      body:
        application/json:
          type: object
```

`api.raml`:

```yaml
#%RAML 1.0
title: Example API
resourceTypes:
  collection: !include resourceTypes/collection.raml
/items:
  type: collection
```

The resolved `GET /items` operation:

```yaml
/items:
  get:
    queryParameters:
      page: integer     # from paged trait in traits.raml
      pageSize: integer # from paged trait in traits.raml
    responses:
      200:
        body:
          application/json:
            type: object  # from collection resource type
```

A trait declared directly in the root API is referenced by its unqualified name only from
within the root API itself:

```yaml
#%RAML 1.0
title: Example API
traits:
  paged:
    queryParameters:
      page: integer
/items:
  get:
    is: [paged]   # written in the root API → resolved against the root API's traits:
```

Resource type names on endpoints are resolved against the root API's `resourceTypes:`.
Both unqualified and qualified (`lib.rtName`) forms are supported via the root API's own
`uses:` map.

`common-resource-types.raml`:

```yaml
#%RAML 1.0 Library
resourceTypes:
  collection:
    get:
      responses:
        200:
          body:
            application/json:
              type: object[]
```

`api.raml`:

```yaml
#%RAML 1.0
title: Example API
uses:
  rt: ./common-resource-types.raml
resourceTypes:
  collection:
    get:
      responses:
        200:
          body:
            application/json:
              type: object
/items:
  type: rt.collection   # qualified — resolved via api.raml's uses:
/users:
  type: collection      # unqualified — resolved against api.raml's resourceTypes:
```

The resolved endpoints:

```yaml
/items:
  get:
    responses:
      200:
        body:
          application/json:
            type: object[]  # from rt.collection in common-resource-types.raml

/users:
  get:
    responses:
      200:
        body:
          application/json:
            type: object    # from collection in api.raml's own resourceTypes:
```

## Installation

### Library

```shell
go get -u github.com/acronis/go-raml
```

### CLI

Go install

```shell
go install github.com/acronis/go-raml/cmd/raml@latest
```

Make install

```shell
make install
```

## Library usage examples

### Parser options

By default, the parser outputs the resulting model as is and without validation. This means that information about all links and inheritance chains
is unmodified. Be aware
that the parser may generate recursive structures, depending on your definition, and you may need to implement recursion
detection when traversing the model.

The parser currently provides the following options:

* `raml.OptWithValidate()` - performs validation of the resulting model (types inheritance validation, types facet
  validations, annotation types and instances validation, examples, defaults, instances, etc.). Also performs unwrap if
  `raml.OptWithUnwrap()` was not specified, but leaves the original types untouched.

* `raml.OptWithUnwrap()` - performs an unwrap of the resulting model and replaces all definitions with unwrapped
  structures. Unwrap resolves the inheritance chain and links and compiles a complete type, with all properties of its
  parents/links.

* `raml.OptWithRawSource()` - retains the raw YAML AST after parsing, for tooling consumers (LSP servers, formatters,
  round-trip transformers). Enables two complementary indices:
  * **Per-fragment node map** — retrieve the `*yaml.Node` root for any parsed fragment via `RAML.GetSourceNode(path)`.
    Useful for per-token position queries (e.g. determining which key a cursor falls inside).
  * **Entity source-info index** — maps every model entity's ID to the key+value `*yaml.Node` pair it was decoded from.
    Retrieve via `RAML.SourceInfo()` and look up individual entities with `SourceInfo.Get(entity.ID)`.

  Non-tooling consumers (validators, code generators) should omit this option to keep memory overhead minimal.

> [!NOTE]
> In most cases, the use of both `OptWithValidate()` and `raml.OptWithUnwrap()` is advised. If you need to access unmodified types, use only `OptWithValidate()`. Memory consumption may be higher and processing time may be longer since `OptWithValidate()` performs a dedicated copy and unwrap for each type.

### Parsing from string

The following code will parse a RAML string, output a library model, and print the common information about the defined
type.

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/acronis/go-raml"
)

func main() {
	// Get current working directory that will serve as a base path
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Define RAML 1.0 Library in a string
	content := `#%RAML 1.0 Library
types:
  BasicType: string
  ChildType:
    type: BasicType
    minLength: 5
`

	// Parse with validation
	// Here we omit OptWithUnwrap to show the difference between child and parent.
	r, err := raml.ParseFromString(content, "library.raml", workDir, raml.OptWithValidate(), raml.OptWithUnwrap())
	if err != nil {
		log.Fatal(err)
	}
	// Cast type to Library since our fragment is RAML 1.0 Library
	lib, _ := r.EntryPoint().(*raml.Library)
	base, _ := lib.Types.Get("ChildType")
	// Cast type to StringShape since child type inherits from a string type
	typ := base.Shape.(*raml.StringShape)
	fmt.Printf(
		"Type name: %s, type: %s, minLength: %d, location: %s\n",
		typ.Name, typ.Type, typ.MinLength.Value, typ.Location,
	)
	// Cast type to StringShape since parent type is string
	parentTyp := base.Inherits[0].Shape.(*raml.StringShape)
	fmt.Printf("Inherits from:\n")
	fmt.Printf(
		"Type name: %s, type: %s, minLength: %d, location: %s\n",
		parentTyp.Name, parentTyp.Type, parentTyp.MinLength.Value, parentTyp.Location,
	)
}
```

The expected output is:

```text
Type name: ChildType, type: string, minLength: 5, location: <absolute_path>/library.raml
Inherits from:
Type name: BasicType, type: string, minLength: 0, location: <absolute_path>/library.raml
```

### Parsing from file

The following code will parse a RAML file, output a library model, and print the common information about the defined
type.

```go
package main

import (
	"fmt"
	"log"

	"github.com/acronis/go-raml"
)

func main() {
	filePath := "<path_to_your_file>"
	r, err := raml.ParseFromPath(filePath, raml.OptWithValidate(), raml.OptWithUnwrap())
	if err != nil {
		log.Fatal(err)
	}
	// Assuming that the parsed fragment is a RAML Library
	lib, _ := r.EntryPoint().(*raml.Library)
	base, _ := lib.Types.Get("BasicType")
	fmt.Printf("Type name: %s, type: %s, location: %s", base.Name, base.Type, base.Location)
}
```

### Validating data against type

Similar to JSON Schema, RAML data types provide a powerful validation mechanism against the defined type.
The following simple example demonstrates how a defined type can be used to validate the values against the type.

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/acronis/go-raml"
)

func main() {
	// Get current working directory that will serve as a base path
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Define RAML 1.0 Library in a string
	content := `#%RAML 1.0 Library
types:
  StringType:
    type: string
    minLength: 5
`

	// Parse with validation
	r, err := raml.ParseFromString(content, "library.raml", workDir, raml.OptWithValidate(), raml.OptWithUnwrap())
	if err != nil {
		log.Fatal(err)
	}
	// Cast type to Library since our fragment is RAML 1.0 Library
	lib, _ := r.EntryPoint().(*raml.Library)
	base, _ := lib.Types.Get("StringType")
	// Cast type to StringShape since defined type is a string
	typ := base.Shape.(*raml.StringShape)
	fmt.Printf(
		"Type name: %s, type: %s, minLength: %d, location: %s\n",
		typ.Name, typ.Type, typ.MinLength.Value, typ.Location,
	)
	fmt.Printf("Empty string: %v\n", base.Validate(""))
	fmt.Printf("Less than 5 characters: %v\n", base.Validate("abc"))
	fmt.Printf("More than 5 characters: %v\n", base.Validate("more than 5 chars"))
	fmt.Printf("Not a string: %v\n", base.Validate(123))
}
```

The expected output is:

```
Empty string: length must be greater than 5
Less than 5 characters: length must be greater than 5
More than 5 characters: <nil>
Not a string: invalid type, got int, expected string
```

## CLI usage examples

Flags:

* `-v` `--verbosity count` - increase verbosity level, one flag for each level, e.g. `-v` for DEBUG
* `-d` `--ensure-duplicates` - ensure that there are no duplicates in tracebacks

### Validate

The `validate` command validates the RAML file against the RAML 1.0 specification.

The following commands will validate the RAML files and output the validation errors.

One file

```bash
raml validate <path_to_your_file>.raml
```

Multiple files

```bash
raml validate <path_to_your_file1>.raml <path_to_your_file2>.raml <path_to_your_file3>.raml
```

Output example

```shell
% raml validate library.raml
[11:46:40.053] INFO: Validating RAML... {
  "path": "library.raml"
}
[11:46:40.060] ERROR: RAML is invalid {
  "tracebacks": {
    "traces": {
      "0": {
        "stack": {
          "0": {
            "message": "unwrap shapes",
            "position": "/tmp/library.raml:1",
            "severity": "error",
            "type": "parsing"
          },
          "1": {
            "message": "unwrap shape",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "unwrapping"
          },
          "2": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "unwrapping"
          },
          "3": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "unwrapping"
          },
          "4": {
            "message": "inherit property: property: a",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          },
          "5": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          },
          "6": {
            "message": "cannot inherit from different type: source: string: target: integer",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          }
        }
      },
      "1": {
        "stack": {
          "0": {
            "message": "validate shapes",
            "position": "/tmp/library.raml:1",
            "severity": "error",
            "type": "parsing"
          },
          "1": {
            "message": "check type",
            "position": "/tmp/common.raml:8:5",
            "severity": "error",
            "type": "validating"
          },
          "2": {
            "message": "minProperties must be less than or equal to maxProperties",
            "position": "/tmp/common.raml:8:5",
            "severity": "error",
            "type": "validating"
          },
          "3": {
            "message": "unwrap shape",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "validating"
          },
          "4": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "unwrapping"
          },
          "5": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:15:5",
            "severity": "error",
            "type": "unwrapping"
          },
          "6": {
            "message": "inherit property: property: a",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          },
          "7": {
            "message": "merge shapes",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          },
          "8": {
            "message": "cannot inherit from different type: source: string: target: integer",
            "position": "/tmp/common.raml:17:10",
            "severity": "error",
            "type": "unwrapping"
          }
        }
      }
    }
  }
}
[11:46:40.092] ERROR: Command failed {
  "error": "errors have been found in the RAML files"
}
```
