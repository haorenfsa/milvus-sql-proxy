package pkg

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Addr     string       `yaml:"addr"`
	Milvus   MilvusConfig `yaml:"milvus"`
	LogLevel string       `yaml:"logLevel"`
	LogPath  string       `yaml:"logPath"`
}

type MilvusConfig struct {
	Address       string `yaml:"addr"`      // Remote address, "localhost:19530".
	Username      string `yaml:"user"`      // Username for auth.
	Password      string `yaml:"pass"`      // Password for auth.
	EnableTLSAuth bool   `yaml:"tlsSecure"` // Enable TLS Auth for transport security.
	APIKey        string `yaml:"apiKey"`    // API key
}

func ParseConfigData(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ParseConfigFile(fileName string) (*Config, error) {
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	return ParseConfigData(data)
}
