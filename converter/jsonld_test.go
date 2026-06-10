package converter

import (
	"context"
	"encoding/json"
	"testing"

	raml "github.com/acronis/go-raml/v3"
	"github.com/stretchr/testify/require"
)

// rawGraph is a parsed @graph array; each node keeps its predicate values as
// raw JSON so structural checks (array vs object) and typed unmarshalling can
// coexist.
type rawGraph []map[string]json.RawMessage

// parseInline parses content directly from memory and returns the populated
// *RAML. baseDir comes from t.TempDir() to satisfy ParseFromString's
// absolute-path requirement, but no files are ever written there — the
// scenario body is read from the in-memory string reader.
func parseInline(t *testing.T, fileName, content string, opts ...raml.ParseOpt) *raml.RAML {
	t.Helper()
	r, err := raml.ParseFromStringCtx(context.Background(), content, fileName, t.TempDir(), opts...)
	require.NoError(t, err, "parse %s", fileName)
	return r
}

// convertToGraph runs the JSON-LD converter and returns the @graph array as
// rawGraph so tests can introspect both shape and field-level structure.
func convertToGraph(t *testing.T, r *raml.RAML) rawGraph {
	t.Helper()
	g, err := NewJSONLDConverter().Convert(r)
	require.NoError(t, err)
	data, err := json.Marshal(g)
	require.NoError(t, err)
	var doc struct {
		Graph rawGraph `json:"@graph"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc.Graph
}

// findNode returns the first node matching pred, or nil.
func (g rawGraph) findNode(pred func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
	for _, n := range g {
		if pred(n) {
			return n
		}
	}
	return nil
}

// findByID returns the node whose @id equals id, or nil.
func (g rawGraph) findByID(id string) map[string]json.RawMessage {
	return g.findNode(func(n map[string]json.RawMessage) bool { return nodeID(n) == id })
}

func hasType(node map[string]json.RawMessage, typeName string) bool {
	var ts []string
	if err := json.Unmarshal(node["@type"], &ts); err != nil {
		// Single-string @type ("rdfs:Seq" enum list nodes).
		var single string
		if json.Unmarshal(node["@type"], &single) == nil {
			return single == typeName
		}
		return false
	}
	for _, t := range ts {
		if t == typeName {
			return true
		}
	}
	return false
}

func nodeID(node map[string]json.RawMessage) string {
	var id string
	_ = json.Unmarshal(node["@id"], &id)
	return id
}

func shName(node map[string]json.RawMessage) string {
	var n string
	_ = json.Unmarshal(node["sh:name"], &n)
	return n
}

// refID extracts the @id from a {"@id": "..."} JSON value.
func refID(raw json.RawMessage) string {
	var ref struct {
		ID string `json:"@id"`
	}
	_ = json.Unmarshal(raw, &ref)
	return ref.ID
}

// refIDs extracts @id values from a [{"@id": "..."}, ...] JSON array.
func refIDs(raw json.RawMessage) []string {
	var refs []struct {
		ID string `json:"@id"`
	}
	_ = json.Unmarshal(raw, &refs)
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// jsonldScenario pairs a small RAML document with an assertion that names a
// specific aspect of the JSON-LD emission to verify.
type jsonldScenario struct {
	name   string
	file   string
	body   string
	unwrap bool
	assert func(t *testing.T, g rawGraph)
}

// scenarios is the canonical list driving both the per-feature assertions and
// the cross-cutting invariants (no duplicate @id, AMF-compatible structure).
// Add a new entry here to extend coverage of any JSON-LD feature dimension.
var scenarios = []jsonldScenario{
	{
		name:   "scalar-datatype-fragment",
		file:   "scalar.raml",
		unwrap: true,
		body: `#%RAML 1.0 DataType
type: string
displayName: A scalar
`,
		assert: func(t *testing.T, g rawGraph) {
			scalar := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "raml-shapes:ScalarShape")
			})
			require.NotNil(t, scalar, "expected a ScalarShape node")

			require.Equal(t, []string{"http://www.w3.org/2001/XMLSchema#string"},
				refIDs(scalar["sh:datatype"]))

			frag := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "raml-shapes:DataTypeFragment")
			})
			require.NotNil(t, frag, "expected a DataTypeFragment root")
			require.Equal(t, nodeID(scalar), refID(frag["doc:encodes"]),
				"fragment's doc:encodes must point at the scalar shape")
		},
	},
	{
		name:   "object-shape-properties",
		file:   "objlib.raml",
		unwrap: true,
		body: `#%RAML 1.0 Library
types:
  User:
    type: object
    properties:
      id:
        type: string
      age?:
        type: integer
`,
		assert: func(t *testing.T, g rawGraph) {
			user := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "sh:NodeShape") && shName(n) == "User"
			})
			require.NotNil(t, user, "expected a NodeShape named User")

			propRefs := refIDs(user["sh:property"])
			require.Len(t, propRefs, 2, "User should have 2 property shapes")

			propByName := map[string]map[string]json.RawMessage{}
			for _, n := range g {
				if hasType(n, "sh:PropertyShape") {
					propByName[shName(n)] = n
				}
			}
			require.Contains(t, propByName, "id")
			require.Contains(t, propByName, "age")

			// sh:path must be an array of @id refs (AMF wire-format).
			idPath := refIDs(propByName["id"]["sh:path"])
			require.Len(t, idPath, 1, "id property must have one sh:path entry")

			// Required vs optional propagates to sh:minCount.
			var idMin, ageMin int
			require.NoError(t, json.Unmarshal(propByName["id"]["sh:minCount"], &idMin))
			require.Equal(t, 1, idMin, "required property has minCount=1")
			if raw, ok := propByName["age"]["sh:minCount"]; ok {
				require.NoError(t, json.Unmarshal(raw, &ageMin))
				require.Equal(t, 0, ageMin, "optional property has minCount=0")
			}

			// raml-shapes:range must resolve to a real shape node.
			require.NotEmpty(t, refID(propByName["id"]["raml-shapes:range"]))
			require.NotNil(t, g.findByID(refID(propByName["id"]["raml-shapes:range"])),
				"PropertyShape's range must reference an existing shape node")
		},
	},
	{
		name:   "array-shape-items",
		file:   "arrlib.raml",
		unwrap: true,
		body: `#%RAML 1.0 Library
types:
  Tags:
    type: array
    items: string
`,
		assert: func(t *testing.T, g rawGraph) {
			arr := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "raml-shapes:ArrayShape") && shName(n) == "Tags"
			})
			require.NotNil(t, arr, "expected an ArrayShape named Tags")

			itemsRef := refID(arr["raml-shapes:items"])
			require.NotEmpty(t, itemsRef, "ArrayShape must declare raml-shapes:items")

			items := g.findByID(itemsRef)
			require.NotNil(t, items, "items reference must resolve to a real node")
			require.True(t, hasType(items, "raml-shapes:ScalarShape"),
				"Tags items should be a ScalarShape (string)")
		},
	},
	{
		name:   "union-shape-anyof",
		file:   "unionlib.raml",
		unwrap: true,
		body: `#%RAML 1.0 Library
types:
  StrOrInt:
    type: string | integer
`,
		assert: func(t *testing.T, g rawGraph) {
			union := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "raml-shapes:UnionShape") && shName(n) == "StrOrInt"
			})
			require.NotNil(t, union, "expected a UnionShape named StrOrInt")

			members := refIDs(union["raml-shapes:anyOf"])
			require.Len(t, members, 2, "string | integer has two union members")

			seenScalars := 0
			for _, ref := range members {
				if hasType(g.findByID(ref), "raml-shapes:ScalarShape") {
					seenScalars++
				}
			}
			require.Equal(t, 2, seenScalars, "both union members must be scalar shapes")
		},
	},
	{
		name:   "enum-as-rdfs-seq-list",
		file:   "enumlib.raml",
		unwrap: true,
		body: `#%RAML 1.0 Library
types:
  Status:
    type: string
    enum: [open, closed]
`,
		assert: func(t *testing.T, g rawGraph) {
			status := g.findNode(func(n map[string]json.RawMessage) bool {
				return shName(n) == "Status" && hasType(n, "raml-shapes:ScalarShape")
			})
			require.NotNil(t, status, "expected a ScalarShape named Status")

			// sh:in must be a single object ref to a list node, NOT an array
			// (AMF distinguishes the list head from the values it contains).
			require.Equal(t, byte('{'), status["sh:in"][0],
				"sh:in must be a single object reference, got %s", status["sh:in"])

			listRef := refID(status["sh:in"])
			list := g.findByID(listRef)
			require.NotNil(t, list, "sh:in target must exist in the graph")
			require.True(t, hasType(list, "rdfs:Seq"), "enum list head must be rdfs:Seq")
			require.NotEmpty(t, list["rdfs:_1"], "rdfs:Seq must have at least one indexed member")
		},
	},
	{
		name:   "named-example-fragment",
		file:   "ex.raml",
		unwrap: true,
		body: `#%RAML 1.0 NamedExample
displayName: Sample user
value:
  id: u-1
  name: Alice
`,
		assert: func(t *testing.T, g rawGraph) {
			frag := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "apiContract:NamedExampleFragment")
			})
			require.NotNil(t, frag, "expected a NamedExampleFragment root")

			exampleRefs := refIDs(frag["apiContract:examples"])
			require.NotEmpty(t, exampleRefs, "fragment must declare apiContract:examples")

			example := g.findByID(exampleRefs[0])
			require.NotNil(t, example, "fragment must point at an existing example node")
			require.True(t, hasType(example, "apiContract:Example"),
				"referenced node must be an apiContract:Example")
			require.NotEmpty(t, example["doc:structuredValue"],
				"Example must carry a doc:structuredValue tree")
		},
	},
	{
		name:   "api-endpoint-operation-params",
		file:   "api.raml",
		unwrap: true,
		body: `#%RAML 1.0
title: Test
/users:
  get:
    description: List users
    queryParameters:
      page:
        type: integer
    headers:
      X-Trace:
        type: string
    responses:
      200:
        body:
          application/json:
            type: string
`,
		assert: func(t *testing.T, g rawGraph) {
			webAPI := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "apiContract:WebAPI")
			})
			require.NotNil(t, webAPI, "expected a WebAPI root")

			endpoint := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "apiContract:EndPoint")
			})
			require.NotNil(t, endpoint, "expected an EndPoint node")
			var path string
			require.NoError(t, json.Unmarshal(endpoint["apiContract:path"], &path))
			require.Equal(t, "/users", path)

			opRefs := refIDs(endpoint["apiContract:supportedOperation"])
			require.Len(t, opRefs, 1, "endpoint must expose one operation")

			op := g.findByID(opRefs[0])
			require.True(t, hasType(op, "apiContract:Operation"))
			var method string
			require.NoError(t, json.Unmarshal(op["apiContract:method"], &method))
			require.Equal(t, "get", method)

			// Drill into request: query parameter + header must be present as
			// separate Parameter nodes.
			expects := refIDs(op["apiContract:expects"])
			require.Len(t, expects, 1)
			req := g.findByID(expects[0])
			require.True(t, hasType(req, "apiContract:Request"))
			require.Len(t, refIDs(req["apiContract:parameter"]), 1, "one query param")
			require.Len(t, refIDs(req["apiContract:header"]), 1, "one header")
		},
	},
	{
		name:   "oauth2-security-scheme",
		file:   "secapi.raml",
		unwrap: true,
		body: `#%RAML 1.0
title: Secure
securitySchemes:
  oauth2:
    type: OAuth 2.0
    settings:
      authorizationUri: https://example.com/auth
      accessTokenUri: https://example.com/token
      authorizationGrants: [authorization_code]
      scopes: [read, write]
/protected:
  get:
    securedBy: [oauth2]
    responses:
      200:
`,
		assert: func(t *testing.T, g rawGraph) {
			scheme := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "security:SecurityScheme") && shName(n) == "oauth2"
			})
			// The declared scheme may be under a different sh:name path;
			// fall back to any SecurityScheme node if needed.
			if scheme == nil {
				scheme = g.findNode(func(n map[string]json.RawMessage) bool {
					return hasType(n, "security:SecurityScheme")
				})
			}
			require.NotNil(t, scheme, "expected a SecurityScheme node")
			var schemeType string
			require.NoError(t, json.Unmarshal(scheme["security:type"], &schemeType))
			require.Equal(t, "OAuth 2.0", schemeType)

			settings := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "security:OAuth2Settings")
			})
			require.NotNil(t, settings, "OAuth 2.0 scheme must emit OAuth2Settings")
			require.NotEmpty(t, refIDs(settings["security:flows"]),
				"OAuth2Settings must declare at least one flow")
		},
	},
	{
		name:   "annotation-type-domain-property",
		file:   "annot.raml",
		unwrap: true,
		body: `#%RAML 1.0 Library
annotationTypes:
  reviewedBy: string
types:
  User:
    type: object
    properties:
      id: string
`,
		assert: func(t *testing.T, g rawGraph) {
			prop := g.findNode(func(n map[string]json.RawMessage) bool {
				return hasType(n, "doc:DomainProperty")
			})
			require.NotNil(t, prop, "annotation type must emit a DomainProperty")
			require.True(t, hasType(prop, "rdf:Property"),
				"DomainProperty must also carry the rdf:Property type")
		},
	},
	{
		name:   "inheritance-preserved-nowrap",
		file:   "inhlib.raml",
		unwrap: false,
		body: `#%RAML 1.0 Library
types:
  Animal:
    type: object
    properties:
      name: string
  Dog:
    type: Animal
    properties:
      breed: string
`,
		assert: func(t *testing.T, g rawGraph) {
			dog := g.findNode(func(n map[string]json.RawMessage) bool {
				return shName(n) == "Dog"
			})
			require.NotNil(t, dog, "expected a Dog declaration node")
			parents := refIDs(dog["raml-shapes:inherits"])
			require.NotEmpty(t, parents,
				"in nowrap mode Dog must declare raml-shapes:inherits")

			// At least one of the parents must resolve to a node named Animal.
			seenAnimal := false
			for _, p := range parents {
				if shName(g.findByID(p)) == "Animal" {
					seenAnimal = true
					break
				}
			}
			require.True(t, seenAnimal, "Dog must inherit from a node named Animal")
		},
	},
	{
		name:   "type-alias-link-target-nowrap",
		file:   "aliaslib.raml",
		unwrap: false,
		body: `#%RAML 1.0 Library
types:
  Original:
    type: string
  Alias: Original
`,
		assert: func(t *testing.T, g rawGraph) {
			alias := g.findNode(func(n map[string]json.RawMessage) bool {
				return shName(n) == "Alias"
			})
			require.NotNil(t, alias, "expected an Alias declaration node")
			require.NotEmpty(t, alias["doc:link-target"],
				"alias must emit doc:link-target in nowrap mode")
			// doc:link-target must be an array (AMF wire-format invariant).
			require.Equal(t, byte('['), alias["doc:link-target"][0],
				"doc:link-target must be an array, got %s", alias["doc:link-target"])
		},
	},
}

// TestJSONLD_FeatureScenarios runs each scenario's per-feature assertion.
// Each entry isolates a single JSON-LD emission concern so a failure
// pinpoints exactly which dimension regressed.
func TestJSONLD_FeatureScenarios(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			opts := []raml.ParseOpt{raml.OptWithValidate()}
			if sc.unwrap {
				opts = append(opts, raml.OptWithUnwrap())
			}
			r := parseInline(t, sc.file, sc.body, opts...)
			sc.assert(t, convertToGraph(t, r))
		})
	}
}

// TestJSONLD_NoDuplicateIDs verifies that no two @graph nodes share the same
// @id, in both unwrap and nowrap modes, across every scenario.
//
// Regression net for intermediate *BaseShape objects that bypass shapeIDs
// registration and accidentally claim a contextID already in use.
func TestJSONLD_NoDuplicateIDs(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for _, unwrap := range []bool{true, false} {
				label := "nowrap"
				if unwrap {
					label = "unwrapped"
				}
				t.Run(label, func(t *testing.T) {
					opts := []raml.ParseOpt{raml.OptWithValidate()}
					if unwrap {
						opts = append(opts, raml.OptWithUnwrap())
					}
					r := parseInline(t, sc.file, sc.body, opts...)
					graph := convertToGraph(t, r)

					seen := make(map[string]bool, len(graph))
					for _, n := range graph {
						id := nodeID(n)
						require.False(t, seen[id], "duplicate @id %q in scenario %q (%s)", id, sc.name, label)
						seen[id] = true
					}
				})
			}
		})
	}
}

// TestJSONLD_AMFStructure verifies the wire-format invariants the AMF JS
// consumers (API Console, AmfHelperMixin) rely on:
//   - sh:datatype     → array of @id objects
//   - doc:link-target → array
//   - sh:path         → array
//   - sh:in           → single @id object (the list head, never an array)
//
// Asserted across every scenario in both unwrap and nowrap modes.
func TestJSONLD_AMFStructure(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for _, unwrap := range []bool{true, false} {
				label := "nowrap"
				if unwrap {
					label = "unwrapped"
				}
				t.Run(label, func(t *testing.T) {
					opts := []raml.ParseOpt{raml.OptWithValidate()}
					if unwrap {
						opts = append(opts, raml.OptWithUnwrap())
					}
					r := parseInline(t, sc.file, sc.body, opts...)
					graph := convertToGraph(t, r)

					for _, node := range graph {
						id := nodeID(node)
						if dt, ok := node["sh:datatype"]; ok {
							require.Equal(t, byte('['), dt[0],
								"node %s: sh:datatype must be an array, got %s", id, dt)
						}
						if lt, ok := node["doc:link-target"]; ok {
							require.Equal(t, byte('['), lt[0],
								"node %s: doc:link-target must be an array, got %s", id, lt)
						}
						if sp, ok := node["sh:path"]; ok {
							require.Equal(t, byte('['), sp[0],
								"node %s: sh:path must be an array, got %s", id, sp)
						}
						if si, ok := node["sh:in"]; ok {
							require.Equal(t, byte('{'), si[0],
								"node %s: sh:in must be a single object, got %s", id, si)
						}
					}
				})
			}
		})
	}
}
