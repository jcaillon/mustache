package render

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type BashLambda string

func DecodeData(node *yaml.Node) (any, error) {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil, nil
		}
		return DecodeData(node.Content[0])
	}
	if node.Kind == yaml.AliasNode {
		return DecodeData(node.Alias)
	}
	if node.Tag == "!code" {
		if node.Kind != yaml.MappingNode || len(node.Content) != 2 || node.Content[0].Value != "bash" {
			return nil, fmt.Errorf("!code must contain exactly one bash command")
		}
		var command string
		if err := node.Content[1].Decode(&command); err != nil {
			return nil, fmt.Errorf("decode !code bash command: %w", err)
		}
		return BashLambda(command), nil
	}
	if !containsCode(node) {
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}

	switch node.Kind {
	case yaml.MappingNode:
		values := make(map[any]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			var key any
			if err := node.Content[i].Decode(&key); err != nil {
				return nil, err
			}
			value, err := DecodeData(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		return values, nil
	case yaml.SequenceNode:
		values := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := DecodeData(child)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	default:
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func containsCode(node *yaml.Node) bool {
	if node.Tag == "!code" {
		return true
	}
	for _, child := range node.Content {
		if containsCode(child) {
			return true
		}
	}
	return node.Alias != nil && containsCode(node.Alias)
}
