package miner

import (
	"encoding/json"
	"os"

	"github.com/republicprotocol/go-identity"
)

// Config information for Miners
type Config struct {
	Host                    string                  `json:"host"`
	Port                    string                  `json:"port"`
	EthereumPrivateKey      string                  `josn:"ethereum_private_key"`
	RepublicKeyPair         string                  `json:"republic_key_pair"`
	RSAKeyPair              string                  `json:"rsa_key_pair"`
	MultiAddress            identity.MultiAddress   `json:"multi_address"`
	BootstrapMultiAddresses identity.MultiAddresses `json:"bootstrap_multi_addresses"`
}

// LoadConfig loads a Config object from the given filename. Returns the Config
// object, or an error.
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	config := new(Config)
	if err := json.NewDecoder(file).Decode(config); err != nil {
		return nil, err
	}
	return config, nil
}
