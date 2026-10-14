package render

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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

func LoadDataFiles(names string) ([]any, error) {
	if names == "" {
		return nil, nil
	}

	files := strings.Split(names, ",")
	documents := make([]any, 0, len(files))
	for _, name := range files {
		if name == "" {
			return nil, errors.New("data file list contains an empty path")
		}
		file, err := os.Open(name)
		if err != nil {
			return nil, fmt.Errorf("open YAML: %w", err)
		}
		document, decodeErr := decodeSingleDocument(file, name)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close YAML %s: %w", name, closeErr)
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func decodeSingleDocument(data io.Reader, name string) (any, error) {
	decoder := yaml.NewDecoder(data)
	var document any
	documentCount := 0
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode YAML %s", FormatYAMLParserError(name, err))
		}
		if EmptyYAMLDocument(&node) {
			continue
		}
		documentCount++
		if documentCount > 1 {
			return nil, fmt.Errorf("YAML %s contains more than one document", name)
		}
		document, err = DecodeData(&node)
		if err != nil {
			return nil, fmt.Errorf("decode YAML %s: %w", name, err)
		}
	}
	if documentCount == 0 {
		return map[any]any{}, nil
	}
	return document, nil
}

func EmptyYAMLDocument(node *yaml.Node) bool {
	return len(node.Content) == 0 || node.Content[0].Tag == "!!null" && node.Content[0].Value == ""
}

func FormatYAMLParserError(name string, err error) string {
	const prefix = "yaml: line "

	message := err.Error()
	rest, ok := strings.CutPrefix(message, prefix)
	if !ok {
		return fmt.Sprintf("%s: %s", name, message)
	}
	lineText, message, ok := strings.Cut(rest, ": ")
	if !ok {
		return fmt.Sprintf("%s: %s", name, err)
	}
	line, parseErr := strconv.Atoi(lineText)
	if parseErr != nil {
		return fmt.Sprintf("%s: %s", name, err)
	}
	return fmt.Sprintf("%s:%d: %s", name, line+1, message)
}

func MergeData(base, override any) any {
	baseMap, baseIsMap := dataMap(base)
	overrideMap, overrideIsMap := dataMap(override)
	if !baseIsMap || !overrideIsMap {
		return override
	}

	merged := make(map[any]any, len(baseMap)+len(overrideMap))
	for key, value := range baseMap {
		merged[key] = value
	}
	for key, value := range overrideMap {
		if baseValue, ok := merged[key]; ok {
			value = MergeData(baseValue, value)
		}
		merged[key] = value
	}
	return merged
}

func dataMap(value any) (map[any]any, bool) {
	switch value := value.(type) {
	case map[any]any:
		return value, true
	case map[string]any:
		converted := make(map[any]any, len(value))
		for key, item := range value {
			converted[key] = item
		}
		return converted, true
	default:
		return nil, false
	}
}
