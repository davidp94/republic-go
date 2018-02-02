package network

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/republicprotocol/go-dht"
	"github.com/republicprotocol/go-do"
	"github.com/republicprotocol/go-identity"
	"github.com/republicprotocol/go-rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// The Delegate is used as a callback interface to inject logic into the
// different RPCs.
type Delegate interface {
	OnPingReceived(from identity.MultiAddress)
	OnQueryCloserPeersReceived(from identity.MultiAddress)
}

// Node implements the gRPC Node service.
type Node struct {
	Delegate
	Server  *grpc.Server
	DHT     *dht.DHT
	Options Options
}

// NewNode returns a Node with the given its own identity.MultiAddress, a list
// of bootstrap node identity.MultiAddresses, and a delegate that defines
// callbacks for each RPC.
func NewNode(delegate Delegate, options Options) *Node {
	return &Node{
		Delegate: delegate,
		Server:   grpc.NewServer(grpc.ConnectionTimeout(30 * time.Second)),
		DHT:      dht.NewDHT(options.MultiAddress.Address(), options.MaxBucketLength),
		Options:  options,
	}
}

// Serve starts the gRPC server.
func (node *Node) Serve() error {
	rpc.RegisterSwarmNodeServer(node.Server, node)
	host, err := node.MultiAddress().ValueForProtocol(identity.IP4Code)
	if err != nil {
		return err
	}
	port, err := node.MultiAddress().ValueForProtocol(identity.TCPCode)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", host, port))
	if err != nil {
		return err
	}
	if node.Options.Debug >= DebugLow {
		log.Printf("Listening at %s:%s\n", host, port)
	}
	return node.Server.Serve(listener)
}

// Stop the gRPC server.
func (node *Node) Stop() {
	if node.Options.Debug >= DebugLow {
		log.Printf("Stopping\n")
	}
	node.Server.Stop()
}

// Bootstrap the Node into the network. The Node will connect to each bootstrap
// Node and attempt to find itself in the network. This process will ultimately
// connect it to Nodes that are close to it in XOR space.
func (node *Node) Bootstrap() {
	if node.Options.Debug >= DebugMedium {
		log.Printf("%v is bootstrapping...\n", node.Address())
	}
	if node.Options.Concurrent {
		do.ForAll(node.Options.BootstrapMultiAddresses, func(i int) {
			node.bootstrapUsingMultiAddress(node.Options.BootstrapMultiAddresses[i])
		})
	} else {
		for _, bootstrapMultiAddress := range node.Options.BootstrapMultiAddresses {
			node.bootstrapUsingMultiAddress(bootstrapMultiAddress)
		}
	}
	if node.Options.Debug >= DebugMedium {
		log.Printf("%v connected to %v peers after bootstrapping.\n", node.Address(), len(node.DHT.MultiAddresses()))
	}
	if node.Options.Debug >= DebugHigh {
		log.Printf("%v connected to %v.\n", node.Address(), node.DHT.MultiAddresses())
	}
}

// Prune an identity.Address from the dht.DHT. Returns a boolean indicating
// whether or not an identity.Address was pruned.
func (node *Node) Prune(target identity.Address) (bool, error) {
	bucket, err := node.DHT.FindBucket(target)
	if err != nil {
		return false, err
	}
	if bucket == nil || bucket.Length() == 0 {
		return false, nil
	}
	multiAddress := bucket.MultiAddresses[0]
	if err := rpc.PingTarget(multiAddress, node.MultiAddress(), time.Minute); err != nil {
		return true, node.DHT.RemoveMultiAddress(multiAddress)
	}
	return false, node.DHT.UpdateMultiAddress(multiAddress)
}

// Address returns the identity.Address of the Node.
func (node *Node) Address() identity.Address {
	return node.Options.MultiAddress.Address()
}

// MultiAddress returns the identity.MultiAddress of the Node.
func (node *Node) MultiAddress() identity.MultiAddress {
	return node.Options.MultiAddress
}

// Ping is used to test the connection to the Node and exchange
// identity.MultiAddresses. If the Node does not respond, or it responds with
// an error, then the connection should be considered unhealthy.
func (node *Node) Ping(ctx context.Context, from *rpc.MultiAddress) (*rpc.Nothing, error) {
	if node.Options.Debug >= DebugHigh {
		log.Printf("%v is receiving a ping...\n", node.Address())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wait := do.Process(func() do.Option {
		nothing, err := node.ping(from)
		if err != nil {
			return do.Err(err)
		}
		return do.Ok(nothing)
	})

	select {
	case val := <-wait:
		if nothing, ok := val.Ok.(*rpc.Nothing); ok {
			return nothing, val.Err
		}
		return &rpc.Nothing{}, val.Err

	case <-ctx.Done():
		return &rpc.Nothing{}, ctx.Err()
	}
}

// QueryCloserPeers is used to return rpc.MultiAddresses that are closer to the
// given target rpc.Address. It will not return rpc.MultiAddresses that are
// further away from the target than the Node itself. The rpc.MultiAddresses
// returned are not guaranteed to provide healthy connections and should be
// pinged.
func (node *Node) QueryCloserPeers(ctx context.Context, query *rpc.Query) (*rpc.MultiAddresses, error) {
	if node.Options.Debug >= DebugHigh {
		log.Printf("%v is querying for closer peers...\n", node.Address())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wait := do.Process(func() do.Option {
		peers, err := node.queryCloserPeers(query)
		if err != nil {
			return do.Err(err)
		}
		return do.Ok(peers)
	})

	select {
	case val := <-wait:
		if multiAddresses, ok := val.Ok.(*rpc.MultiAddresses); ok {
			return multiAddresses, val.Err
		}
		return &rpc.MultiAddresses{Multis: []*rpc.MultiAddress{}}, val.Err

	case <-ctx.Done():
		return &rpc.MultiAddresses{Multis: []*rpc.MultiAddress{}}, ctx.Err()
	}
}

// QueryCloserPeersOnFrontier is used to return the closest rpc.MultiAddresses
// that can be reached from this Node, given target rpc.Address. It will not
// return rpc.MultiAddresses that are further away from the target than the
// Node itself. The rpc.MultiAddresses returned are not guaranteed to provide
// healthy connections and should be pinged.
func (node *Node) QueryCloserPeersOnFrontier(ctx context.Context, query *rpc.Query) (*rpc.MultiAddresses, error) {
	if node.Options.Debug >= DebugHigh {
		log.Printf("%v is querying for closer peers on frontier...\n", node.Address())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wait := do.Process(func() do.Option {
		peers, err := node.queryCloserPeersOnFrontier(query)
		if err != nil {
			return do.Err(err)
		}
		return do.Ok(peers)
	})

	select {
	case val := <-wait:
		if multiAddresses, ok := val.Ok.(*rpc.MultiAddresses); ok {
			return multiAddresses, val.Err
		}
		return &rpc.MultiAddresses{Multis: []*rpc.MultiAddress{}}, val.Err

	case <-ctx.Done():
		return &rpc.MultiAddresses{Multis: []*rpc.MultiAddress{}}, ctx.Err()
	}
}

func (node *Node) ping(from *rpc.MultiAddress) (*rpc.Nothing, error) {
	// Update the DHT.
	fromMultiAddress, err := rpc.DeserializeMultiAddress(from)
	if err != nil {
		return &rpc.Nothing{}, err
	}

	// Notify the delegate of the ping.
	node.Delegate.OnPingReceived(fromMultiAddress)
	return &rpc.Nothing{}, node.updatePeer(from)
}

func (node *Node) queryCloserPeers(query *rpc.Query) (*rpc.MultiAddresses, error) {
	// Get the target identity.Address for which this Node is searching for
	// peers.
	target := identity.Address(query.Query.Address)
	peers, err := node.DHT.FindMultiAddressNeighbors(target, node.Options.Alpha)
	if err != nil {
		return &rpc.MultiAddresses{Multis: []*rpc.MultiAddress{}}, err
	}

	// Filter away peers that are further from the target than this Node.
	peersCloserToTarget := make(identity.MultiAddresses, 0, len(peers))
	for _, peer := range peers {
		closer, err := identity.Closer(peer.Address(), node.Address(), target)
		if err != nil {
			return rpc.SerializeMultiAddresses(peersCloserToTarget), err
		}
		if closer {
			peersCloserToTarget = append(peersCloserToTarget, peer)
		}
	}

	return rpc.SerializeMultiAddresses(peersCloserToTarget), node.updatePeer(query.From)
}

func (node *Node) queryCloserPeersOnFrontier(query *rpc.Query) (*rpc.MultiAddresses, error) {

	// Get the target identity.Address for which this Node is searching for
	// peers.
	target := identity.Address(query.Query.Address)
	peers := node.DHT.MultiAddresses()

	// Filter away peers that are further from the target than this Node.
	peersCloserToTarget := make(identity.MultiAddresses, 0, len(peers))
	for _, peer := range peers {
		closer, err := identity.Closer(peer.Address(), node.Address(), target)
		if err != nil {
			return rpc.SerializeMultiAddresses(peersCloserToTarget), err
		}
		if closer {
			peersCloserToTarget = append(peersCloserToTarget, peer)
		}
	}

	// Create the frontier and a closure map.
	frontier := append(identity.MultiAddresses{}, peersCloserToTarget...)
	black := make(map[identity.Address]struct{})
	white := make(map[identity.Address]struct{})
	// Immediately close the Node that is running this query and mark all peers
	// in the frontier as seen.
	black[node.Address()] = struct{}{}
	for _, peer := range frontier {
		white[peer.Address()] = struct{}{}
	}

	// While there are still Nodes to be explored in the frontier.
	for len(frontier) > 0 {
		// Pop the first peer off the frontier.
		peer := frontier[0]
		frontier = frontier[1:]

		// Close the peer and use it to find peers that are even closer to the
		// target.
		black[peer.Address()] = struct{}{}
		if peer.Address() == target {
			continue
		}
		// log.Printf("[%v] querying", node.Address())
		candidates, err := rpc.QueryCloserPeersFromTarget(peer, node.MultiAddress(), target, time.Second)
		if err != nil {
			continue
		}
		// log.Printf("[%v] querying done!", node.Address())

		// Filter any candidate that is already in the closure.
		for _, candidate := range candidates {
			if _, ok := black[candidate.Address()]; ok {
				continue
			}
			if _, ok := white[candidate.Address()]; ok {
				continue
			}
			// Expand the frontier by candidates that have not already been
			// explored, and store them in a persistent list of close peers.
			frontier = append(frontier, candidate)
			peersCloserToTarget = append(peersCloserToTarget, candidate)
			white[candidate.Address()] = struct{}{}
		}
	}

	return rpc.SerializeMultiAddresses(peersCloserToTarget), node.updatePeer(query.From)
}

func (node *Node) bootstrapUsingMultiAddress(bootstrapMultiAddress identity.MultiAddress) error {
	var err error
	var peers identity.MultiAddresses

	// The Node attempts to find itself in the network with three attempts
	// backing off by 10 seconds per attempt.
	for attempt := 0; attempt < 3; attempt++ {
		// Query the bootstrap node.
		peers, err = rpc.QueryCloserPeersOnFrontierFromTarget(
			bootstrapMultiAddress,
			node.MultiAddress(),
			node.Address(),
			node.Options.Timeout+time.Duration(attempt)*node.Options.TimeoutStep,
		)
		// Errors are not returned because it is reasonable that a bootstrap
		// Node might be unavailable at this time.
		if err == nil {
			break
		}
		if node.Options.Debug >= DebugLow {
			log.Println(err)
		}
		if attempt == 2 {
			return err
		}
	}

	// Peers returned by the query will be added to the DHT.
	if node.Options.Debug >= DebugMedium {
		log.Printf("%v received %v peers from %v.\n", node.Address(), len(peers), bootstrapMultiAddress.Address())
	}
	for _, peer := range peers {
		if peer.Address() == node.Address() {
			continue
		}
		if err := node.DHT.UpdateMultiAddress(peer); err != nil {
			if node.Options.Debug >= DebugLow {
				log.Println(err)
			}
		}
	}
	return nil
}

func (node *Node) updatePeer(peer *rpc.MultiAddress) error {
	multiAddress, err := rpc.DeserializeMultiAddress(peer)
	if err != nil {
		return err
	}
	if multiAddress.Address() == node.Address() {
		return nil
	}
	if err := node.DHT.UpdateMultiAddress(multiAddress); err != nil {
		if err == dht.ErrFullBucket {
			pruned, err := node.Prune(multiAddress.Address())
			if err != nil {
				return err
			}
			if pruned {
				return node.DHT.UpdateMultiAddress(multiAddress)
			}
			return nil
		}
		return err
	}
	return nil
}
