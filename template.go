package raml

import (
	"strings"

	"github.com/acronis/go-stacktrace"
	"gopkg.in/yaml.v3"
)

func parseTemplateVariables(location, s string) ([]VariableInfo, error) {
	var vars []VariableInfo

	for {
		start := strings.Index(s, "<<")
		if start < 0 {
			break
		}

		rest := s[start+2:]

		relEnd := strings.Index(rest, ">>")
		if relEnd < 0 {
			return nil, StacktraceNew(
				"unclosed template variable",
				location,
			)
		}

		end := start + 2 + relEnd

		content := s[start+2 : end]

		name, actions, err := parseVariableContent(location, content)
		if err != nil {
			return nil, StacktraceNewWrapped(
				"parse variable content",
				err,
				location,
			)
		}

		vars = append(vars, VariableInfo{
			Name:      name,
			Substring: s[start : end+2],
			Actions:   actions,
		})

		s = s[end+2:]
	}

	return vars, nil
}

func parseVariableContent(location, content string) (string, []string, error) {
	start := 0
	first := true

	var (
		name    string
		actions []string
	)

	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '|' {
			a := start
			b := i

			for a < b && content[a] == ' ' {
				a++
			}

			for b > a && content[b-1] == ' ' {
				b--
			}

			if a == b {
				start = i + 1
				continue
			}

			part := content[a:b]

			if first {
				if part[0] == '!' {
					return "", nil, StacktraceNew(
						"action without variable name",
						location,
					)
				}

				name = part
				first = false
			} else {
				if part[0] != '!' {
					return "", nil, StacktraceNew(
						"invalid action, must start with '!'",
						location,
						stacktrace.WithInfo("action", part),
					)
				}

				if _, ok := SetOfActions[part]; !ok {
					return "", nil, StacktraceNew(
						"unknown action",
						location,
						stacktrace.WithInfo("action", part),
					)
				}

				actions = append(actions, part)
			}

			start = i + 1
		}
	}

	if first {
		return "", nil, StacktraceNew(
			"missing variable name",
			location,
		)
	}

	return name, actions, nil
}

// collectVariablesIndex recursively collects variable indices from a YAML node tree.
// It calls findVariable for each scalar node and stores the results in NodeVariableIndex.
func collectVariablesIndex(
	location string,
	node *yaml.Node,
	idx int,
	nodeVariableIndex map[int][]VariableInfo,
	declaredVariables map[string]struct{},
) error {
	if node.Kind == yaml.ScalarNode {
		if err := findVariable(location, node, idx, nodeVariableIndex, declaredVariables); err != nil {
			return StacktraceNewWrapped("find variable", err, location, WithNodePosition(node))
		}
	}
	for i := 0; i < len(node.Content); i++ {
		if err := collectVariablesIndex(location, node.Content[i], idx+i, nodeVariableIndex, declaredVariables); err != nil {
			return StacktraceNewWrapped("collect variables index", err, location,
				WithNodePosition(node.Content[i]))
		}
	}
	return nil
}

// findVariable finds template variables in a scalar node and stores them in the index.
func findVariable(
	location string,
	node *yaml.Node,
	idx int,
	nodeVariableIndex map[int][]VariableInfo,
	declaredVariables map[string]struct{},
) error {
	if node.Kind != yaml.ScalarNode {
		return StacktraceNew(
			"variable must be a scalar node",
			location,
			WithNodePosition(node),
		)
	}

	if node.Tag != TagStr {
		return nil
	}

	vars, err := parseTemplateVariables(location, node.Value)
	if err != nil {
		return StacktraceNewWrapped(
			"parse template variables",
			err,
			location,
			WithNodePosition(node),
		)
	}

	if len(vars) == 0 {
		return nil
	}

	for _, v := range vars {
		declaredVariables[v.Name] = struct{}{}
	}

	nodeVariableIndex[idx] = vars

	return nil
}

// collectRequiredVariables collects all variable names from a YAML node tree.
// This is used to determine which parameters are actually required after optional methods are removed.
func collectRequiredVariables(node *yaml.Node, idx int, nodeVariableIndex map[int][]VariableInfo) map[string]struct{} {
	vars := make(map[string]struct{})
	if node == nil {
		return vars
	}
	if node.Kind == yaml.ScalarNode {
		if variables, exists := nodeVariableIndex[idx]; exists {
			for _, variable := range variables {
				vars[variable.Name] = struct{}{}
			}
		}
		return vars
	}
	for i := 0; i < len(node.Content); i++ {
		for name := range collectRequiredVariables(node.Content[i], idx+i, nodeVariableIndex) {
			vars[name] = struct{}{}
		}
	}
	return vars
}
