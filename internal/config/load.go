package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "configuration is invalid"
	}
	return fmt.Sprintf("configuration is invalid: %s: %s", e.Problems[0].Path, e.Problems[0].Message)
}

func Discover(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "jumpotp", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "jumpotp", "config.yaml"), nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	result, err := Parse(data)
	if err != nil {
		return nil, err
	}
	result.Path = path
	return result, nil
}

func Parse(data []byte) (*Config, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("configuration must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode trailing YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("configuration root must be a mapping")
	}
	var structural []Problem
	inspectYAML(document.Content[0], "", &structural)
	if len(structural) > 0 {
		return nil, &ValidationError{Problems: structural}
	}

	var result Config
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode strict YAML: %w", err)
	}
	result.ApplyDefaults()
	if problems := result.Validate(); len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	return &result, nil
}

func inspectYAML(node *yaml.Node, path string, problems *[]Problem) {
	add := func(code, valuePath, message string) {
		*problems = append(*problems, Problem{Code: code, Path: valuePath, Message: message})
	}
	if node.Kind == yaml.AliasNode {
		add("yaml_alias", displayPath(path), "aliases are not supported")
		return
	}
	if node.Tag != "" && !strings.HasPrefix(node.Tag, "tag:yaml.org,2002:") && !strings.HasPrefix(node.Tag, "!!") {
		add("yaml_tag", displayPath(path), "custom YAML tags are not supported")
	}
	switch node.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				add("invalid_key", displayPath(path), "mapping keys must be strings")
				continue
			}
			childPath := key.Value
			if path != "" {
				childPath = path + "." + key.Value
			}
			if key.Value == "<<" {
				add("yaml_merge", displayPath(childPath), "YAML merge keys are not supported")
			} else if seen[key.Value] {
				add("duplicate_key", displayPath(childPath), "duplicate mapping key")
			}
			seen[key.Value] = true
			inspectYAML(value, childPath, problems)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			inspectYAML(child, fmt.Sprintf("%s[%d]", path, index), problems)
		}
	}
}

func displayPath(path string) string {
	if path == "" {
		return "$"
	}
	return path
}
