package config

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// tomlKeys lists the toml tags of a struct's fields, leaving out "-".
func tomlKeys(v any) []string {
	var out []string
	t := reflect.TypeOf(v)
	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("toml"); tag != "" && tag != "-" {
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

func schemaKeys(props map[string]json.RawMessage) []string {
	var out []string
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The JSON Schema in the repo root must list exactly the settings that
// Config, Repos and Profile read.
func TestSchemaMatchesStructs(t *testing.T) {
	data, err := os.ReadFile("../../config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	type object struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	var s struct {
		object
		Definitions struct {
			Profile object `json:"profile"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	var repos object
	if err := json.Unmarshal(s.Properties["repos"], &repos); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name         string
		schema, toml []string
	}{
		{"top level", schemaKeys(s.Properties), tomlKeys(Config{})},
		{"repos", schemaKeys(repos.Properties), tomlKeys(Repos{})},
		{"profile", schemaKeys(s.Definitions.Profile.Properties), tomlKeys(Profile{})},
	} {
		if !slices.Equal(c.schema, c.toml) {
			t.Errorf("%s: schema keys %v, toml tags %v", c.name, c.schema, c.toml)
		}
	}
}
