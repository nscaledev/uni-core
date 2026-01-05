/*
Copyright 2024-2025 the Unikorn Authors.
Copyright 2026 Nscale.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"os"
	"text/template"

	"github.com/go-openapi/jsonpointer"

	"sigs.k8s.io/yaml"
)

type Config struct {
	// Variables is a list of variable to set.
	Variables []Variable `json:"variables"`
	// Files is a list of files to apply templating to.
	Files []File `json:"files"`
}

type Variable struct {
	// Name of the variable.
	Name string `json:"name"`
	// Yaml source of the variable.
	Yaml *YamlVariable `json:"yaml,omitempty"`
}

type YamlVariable struct {
	// File is the path to the source.
	File string `json:"file"`
	// Pointer is a JSON pointer to the source data.
	Pointer string `json:"pointer"`
}

type File struct {
	// In is a path to the input template.
	In string `json:"in"`
	// Out is a path to the output.
	Out string `json:"out"`
}

func loadConfig() (*Config, error) {
	configRaw, err := os.ReadFile("docs-generate.yaml")
	if err != nil {
		return nil, err
	}

	config := &Config{}

	if err := yaml.Unmarshal(configRaw, config); err != nil {
		return nil, err
	}

	return config, nil
}

func loadVariables(config *Config) (map[string]any, error) {
	variables := map[string]any{}

	for _, variable := range config.Variables {
		//nolint:gocritic
		switch {
		case variable.Yaml != nil:
			data, err := os.ReadFile(variable.Yaml.File)
			if err != nil {
				return nil, err
			}

			var document any

			if err := yaml.Unmarshal(data, &document); err != nil {
				return nil, err
			}

			pointer, err := jsonpointer.New(variable.Yaml.Pointer)
			if err != nil {
				return nil, err
			}

			value, _, err := pointer.Get(document)
			if err != nil {
				return nil, err
			}

			variables[variable.Name] = value
		}
	}

	return variables, nil
}

func renderFile(file File, variables map[string]any) error {
	template, err := template.ParseFiles(file.In)
	if err != nil {
		return err
	}

	out, err := os.Create(file.Out)
	if err != nil {
		return err
	}

	defer out.Close()

	if _, err := out.WriteString("<!-- THIS FILE IS AUTO-GENERATED. DO NOT EDIT -->\n\n"); err != nil {
		return err
	}

	if err := template.Execute(out, variables); err != nil {
		return err
	}

	return nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	variables, err := loadVariables(config)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, file := range config.Files {
		if err := renderFile(file, variables); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}
