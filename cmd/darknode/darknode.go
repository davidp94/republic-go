package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/republicprotocol/republic-go/blockchain/ethereum"
	"github.com/republicprotocol/republic-go/blockchain/ethereum/dnr"
	"github.com/republicprotocol/republic-go/blockchain/ethereum/ledger"
	"github.com/republicprotocol/republic-go/cal"
	"github.com/republicprotocol/republic-go/crypto"
	"github.com/republicprotocol/republic-go/darknode"
	"github.com/republicprotocol/republic-go/dht"
	"github.com/republicprotocol/republic-go/grpc"
	"github.com/republicprotocol/republic-go/identity"
	"github.com/republicprotocol/republic-go/swarm"
)

func main() {

	// Parse command-line arguments
	configParam := flag.String("config", path.Join(os.Getenv("HOME"), ".darknode/config.json"), "JSON configuration file")
	flag.Parse()

	// Load configuration file
	conf, err := darknode.NewConfigFromJSONFile(*configParam)
	if err != nil {
		log.Fatalf("cannot load config: %v", err)
	}

	// Get IP-address
	ipAddr, err := getIPAddress()
	if err != nil {
		log.Fatalf("cannot get ip-address: %v", err)
	}

	// Get multi-address
	multiAddr, err := identity.NewMultiAddressFromString(fmt.Sprintf("/ip4/%s/tcp/18514/republic/%s", ipAddr, conf.Address))
	if err != nil {
		log.Fatalf("cannot get multiaddress: %v", err)
	}
	log.Printf("address %v", multiAddr)

	// Get ethereum bindings
	auth, _, _, _, _, err := getEthereumBindings(conf.Keystore, conf.Ethereum)
	if err != nil {
		log.Fatalf("cannot get ethereum bindings: %v", err)
	}
	log.Printf("ethereum %v", auth.From.Hex())

	server := grpc.NewServer()
	crypter := crypto.NewWeakCrypter()
	dht := dht.NewDHT(conf.Address, 32)
	connPool := grpc.NewConnPool(128)
	newStatus(&dht, server)
	_ = newSwarmer(&crypter, multiAddr, &dht, &connPool, server)

	log.Printf("listening on %v:%v...", conf.Host, conf.Port)
	lis, err := net.Listen("tcp", fmt.Sprintf("%v:%v", conf.Host, conf.Port))
	if err != nil {
		log.Fatalf("cannot listen on %v:%v: %v", conf.Host, conf.Port, err)
	}
	if err := server.Serve(lis); err != nil {
		log.Fatalf("cannot serve on %v:%v: %v", conf.Host, conf.Port, err)
	}
}

func newStatus(dht *dht.DHT, server *grpc.Server) {
	service := grpc.NewStatusService(dht)
	service.Register(server)
}

func newSwarmer(crypter crypto.Crypter, multiAddr identity.MultiAddress, dht *dht.DHT, connPool *grpc.ConnPool, server *grpc.Server) swarm.Swarmer {
	client := grpc.NewSwarmClient(crypter, multiAddr, connPool)
	service := grpc.NewSwarmService(crypter, swarm.NewServer(client, dht))
	service.Register(server)
	return swarm.NewSwarmer(client, dht)
}

func getIPAddress() (string, error) {

	out, err := exec.Command("curl", "https://ipinfo.io/ip").Output()
	out = []byte(strings.Trim(string(out), "\n "))
	if err != nil {
		return "", err
	}
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func getEthereumBindings(keystore crypto.Keystore, conf ethereum.Config) (*bind.TransactOpts, cal.Darkpool, cal.DarkpoolAccounts, cal.DarkpoolFees, cal.RenLedger, error) {
	conn, err := ethereum.Connect(conf)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("cannot connect to ethereum: %v", err)
	}
	auth := bind.NewKeyedTransactor(keystore.EcdsaKey.PrivateKey)
	auth.GasPrice = big.NewInt(1000000000)

	darkpool, err := dnr.NewDarknodeRegistry(context.Background(), conn, auth, &bind.CallOpts{})
	if err != nil {
		fmt.Println(fmt.Errorf("cannot bind to darkpool: %v", err))
		return auth, nil, nil, nil, nil, err
	}

	renLedger, err := ledger.NewRenLedgerContract(context.Background(), conn, auth, &bind.CallOpts{})
	if err != nil {
		fmt.Println(fmt.Errorf("cannot bind to ren ledger: %v", err))
		return auth, nil, nil, nil, nil, err
	}

	return auth, &darkpool, nil, nil, &renLedger, nil
}
