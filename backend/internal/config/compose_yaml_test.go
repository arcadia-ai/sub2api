package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDeploymentYAMLFilesParse(t *testing.T) {
	files := []string{
		"config.example.yaml",
		"docker-compose.yml",
		"docker-compose.dev.yml",
		"docker-compose.local.yml",
		"docker-compose.standalone.yml",
		"docker-compose.custom.yml",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", name))
			require.NoError(t, err)
			var document yaml.Node
			require.NoError(t, yaml.Unmarshal(content, &document))
		})
	}
}
