package pkg

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Mode                string `yaml:"mode"`
	PostgresAddr        string `yaml:"postgresAddr"`
	User                string `yaml:"user"`
	Password            string `yaml:"password"`
	TLSCert             string `yaml:"tlsCert"`
	TLSKey              string `yaml:"tlsKey"`
	QueryTimeoutSeconds int    `yaml:"queryTimeoutSeconds"`

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
	if err := cfg.defaults(); err != nil {
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

func (c *Config) defaults() error {
	if c.Mode == "" {
		c.Mode = "mysql"
	}
	if c.Mode != "mysql" && c.Mode != "postgres" && c.Mode != "both" {
		return fmt.Errorf("mode must be mysql, postgres or both")
	}
	if c.Addr == "" {
		c.Addr = "127.0.0.1:3306"
	}
	if c.PostgresAddr == "" {
		c.PostgresAddr = "127.0.0.1:5432"
	}
	if c.User == "" {
		c.User = "root"
	}
	if c.QueryTimeoutSeconds == 0 {
		c.QueryTimeoutSeconds = 30
	}
	if c.QueryTimeoutSeconds < 0 {
		return fmt.Errorf("queryTimeoutSeconds must be positive")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("tlsCert and tlsKey must both be provided")
	}
	return nil
}
