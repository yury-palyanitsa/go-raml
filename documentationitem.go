package raml

import (
	"fmt"
	"io"
	"log/slog"

	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gopkg.in/yaml.v3"

	"github.com/acronis/go-stacktrace"
)

type DocumentationItem struct {
	ID int64

	Title   *Node[string]
	Content *Node[string]

	Link *DocumentationItemFragment

	CustomDomainProperties *orderedmap.OrderedMap[string, *DomainExtension]

	Location string
	KeyPos   stacktrace.Position
	ValuePos stacktrace.Position
	raml     *RAML
}

func (di *DocumentationItem) decode(node *yaml.Node) error {
	hasTitle := false
	hasContent := false
	for i := 0; i != len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		fragmentPath, rn, err := di.raml.resolveInclude(valueNode, di.Location)
		if err != nil {
			return StacktraceNewWrapped("resolve include", err, di.Location, WithNodePosition(valueNode))
		}
		switch keyNode.Value {
		case FacetTitle:
			hasTitle = true
			var title string
			if err := rn.Decode(&title); err != nil {
				return StacktraceNewWrapped("decode title", err, di.Location, WithNodePosition(valueNode))
			}
			if title == "" {
				return StacktraceNew("title must not be empty", di.Location, WithNodePosition(keyNode))
			}
			di.Title = MakeNode(title, keyNode, valueNode, di.Location, fragmentPath)
		case FacetContent:
			hasContent = true
			var content string
			if err := rn.Decode(&content); err != nil {
				return StacktraceNewWrapped("decode content", err, di.Location, WithNodePosition(valueNode))
			}
			if content == "" {
				return StacktraceNew("content must not be empty", di.Location, WithNodePosition(keyNode))
			}
			di.Content = MakeNode(content, keyNode, valueNode, di.Location, fragmentPath)
		default:
			if IsCustomDomainExtensionNode(keyNode.Value) {
				de, err := di.raml.unmarshalCustomDomainExtension(di.Location, keyNode, valueNode)
				if err != nil {
					return StacktraceNewWrapped("unmarshal custom domain extension", err, di.Location, WithNodePosition(valueNode))
				}
				di.CustomDomainProperties.Set(de.Name, de)
			} else {
				return StacktraceNew("unknown field", di.Location, WithNodePosition(keyNode), stacktrace.WithInfo("field", keyNode.Value))
			}
		}
	}
	// Validate that both title and content are present
	if !hasTitle {
		return StacktraceNew("title is required", di.Location, WithNodePosition(node))
	}
	if !hasContent {
		return StacktraceNew("content is required", di.Location, WithNodePosition(node))
	}
	return nil
}

func (r *RAML) unmarshalDocumentationItems(k, v *yaml.Node, location string) ([]*DocumentationItem, error) {
	if v.Kind != yaml.SequenceNode {
		return nil, StacktraceNew("documentation item must be a mapping node", location, WithNodePosition(k))
	}

	documentationItems := make([]*DocumentationItem, len(v.Content))
	for i, itemNode := range v.Content {
		documentationItem, err := r.unmarshalDocumentationItem(itemNode, location)
		if err != nil {
			return nil, StacktraceNewWrapped("unmarshal documentation item", err, location, WithNodePosition(itemNode))
		}
		documentationItems[i] = documentationItem
	}
	return documentationItems, nil
}

func (r *RAML) unmarshalDocumentationItem(node *yaml.Node, location string) (*DocumentationItem, error) {
	if node.Tag == TagInclude {
		frag, err := r.parseDocumentationItemFragment(r.noteIncludeRef(node, location))
		if err != nil {
			return nil, StacktraceNewWrapped("parse documentation item fragment", err, location, WithNodePosition(node))
		}
		return frag.DocumentationItem, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, StacktraceNew("documentation item must be a mapping node", location, WithNodePosition(node))
	}

	documentationItem := &DocumentationItem{
		ID:                     r.generateSequenceID(),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		raml:                   r,
		Location:               location,
		KeyPos:                 NewNodePosition(node),
		ValuePos:               NewNodePosition(node),
	}
	r.storeEntityNode(documentationItem.ID, nil, node)
	if err := documentationItem.decode(node); err != nil {
		return nil, StacktraceNewWrapped("decode documentation item", err, location, WithNodePosition(node))
	}
	return documentationItem, nil
}

func (di *DocumentationItemFragment) GetLocation() string { return di.Location }

func (di *DocumentationItemFragment) GetReferenceType(refName string) (*BaseShape, error) {
	return resolveLibraryReference(di.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

func (di *DocumentationItemFragment) GetReferenceAnnotationType(refName string) (*BaseShape, error) {
	if ref, err := resolveLibraryReference(di.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.AnnotationTypes.Get(suffix)
	}); err == nil {
		return ref, nil
	}
	return resolveLibraryReference(di.Uses, refName, func(lib *Library, suffix string) (*BaseShape, bool) {
		return lib.Types.Get(suffix)
	})
}

func (di *DocumentationItemFragment) GetTraitDefinition(refName string) (*TraitDefinition, error) {
	return resolveLibraryReference(di.Uses, refName, func(lib *Library, suffix string) (*TraitDefinition, bool) {
		return lib.Traits.Get(suffix)
	})
}

func (di *DocumentationItemFragment) GetResourceTypeDefinition(refName string) (*ResourceTypeDefinition, error) {
	return resolveLibraryReference(di.Uses, refName, func(lib *Library, suffix string) (*ResourceTypeDefinition, bool) {
		return lib.ResourceTypes.Get(suffix)
	})
}

func (di *DocumentationItemFragment) GetSecuritySchemeDefinition(_ string) (*SecuritySchemeDefinition, error) {
	return nil, fmt.Errorf("documentation item does not define security schemes")
}

func (di *DocumentationItemFragment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return StacktraceNew("documentation item must be a mapping node", di.Location, WithNodePosition(node))
	}
	filtered, uses, err := di.raml.filterFragmentUses(node, di.Location)
	if err != nil {
		return StacktraceNewWrapped("filter fragment uses", err, di.Location)
	}
	di.Uses = uses
	item := &DocumentationItem{
		ID:                     di.raml.generateSequenceID(),
		CustomDomainProperties: orderedmap.New[string, *DomainExtension](0),
		raml:                   di.raml,
		Location:               di.Location,
	}
	if err := item.decode(filtered); err != nil {
		return StacktraceNewWrapped("decode documentation item", err, di.Location, WithNodePosition(filtered))
	}
	di.DocumentationItem = item
	return nil
}

func (r *RAML) parseDocumentationItemFragment(path string) (*DocumentationItemFragment, error) {
	cached, f, err := r.parseFragmentCached(path, FragmentDocumentationItem)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return cached.(*DocumentationItemFragment), nil //nolint:errcheck // type is guaranteed by parseFragmentCached+PutFragment contract
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Error("close file error", "error", cerr)
		}
	}()
	return r.decodeDocumentationItemFragment(f, path)
}

func (r *RAML) decodeDocumentationItemFragment(f io.Reader, path string) (*DocumentationItemFragment, error) {
	diFrag := r.MakeDocumentationItemFragment(path)
	// Typed fragments are self-contained: all external type dependencies must be
	// declared via uses:. Inheriting the caller's anchorFrag would allow unqualified
	// references to resolve against the including document's namespace — an antipattern
	// that makes fragments semantically non-standalone and breaks the fragment cache.
	r.pushParseCtx(ParseCtx{AnchorFrag: diFrag})
	defer r.popParseCtx()
	node, err := decodeYAMLFragment(f, diFrag)
	r.storeSourceNode(path, node)
	if err != nil {
		return nil, StacktraceNewWrapped("decode documentation item fragment", err, path,
			stacktrace.WithType(StacktraceTypeParsing))
	}
	r.PutFragment(path, diFrag)
	if st := r.resolveUses(diFrag.Uses, diFrag.Location); st != nil {
		return nil, st
	}
	return diFrag, nil
}
