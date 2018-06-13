package status

import (
	"sync"

	"github.com/republicprotocol/republic-go/dht"
	"github.com/republicprotocol/republic-go/identity"
)

// Writer will write the address
type Writer interface {
	WriteMultiAddress(multiAddress identity.MultiAddress) error
	WriteEthereumAddress(ethAddress string) error
}

// Reader the address
type Reader interface {
	MultiAddress() (identity.MultiAddress, error)
	EthereumAddress() (string, error)
	Peers() (int, error)
}

/*

Basic information
* address
* multiaddress
* ethereum address
* connected peers
* basic funds amounts, balance, e.g. eth
* fees it's earned
* and register or deregister a dark node (deregister is disabled metamask address is not the owner)

*/

type Provider interface {
	Writer
	Reader
}

type provider struct {
	mu              *sync.Mutex
	dht             *dht.DHT
	multiAddress    identity.MultiAddress
	ethereumAddress string
}

// NewProvider returns a new provider
func NewProvider(dht *dht.DHT) Provider {
	return &provider{
		mu:  new(sync.Mutex),
		dht: dht,
	}
}

func (sp *provider) WriteMultiAddress(multiAddr identity.MultiAddress) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.multiAddress = multiAddr
	return nil
}

func (sp *provider) MultiAddress() (identity.MultiAddress, error) {
	return sp.multiAddress, nil
}

// WriteEthereumAddress writes ethAddr to the provider
func (sp *provider) WriteEthereumAddress(ethAddr string) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.ethereumAddress = ethAddr
	return nil
}

// EthereumAddress gets the ethereum address
func (sp *provider) EthereumAddress() (string, error) {
	return sp.ethereumAddress, nil
}

// Peers returns the number of peers the darknode is connected to
func (sp *provider) Peers() (int, error) {
	return len(sp.dht.MultiAddresses()), nil
}
