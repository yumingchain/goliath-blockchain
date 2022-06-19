package sequencer

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

type SequencerMode uint
const (
	PrimaryMode SequencerMode = iota
	ReplicaMode
)

type SequencerNode struct {
	Seq *SequencerCore
	P2P *P2PNode
	RPC *RPCNode
	Mode SequencerMode
}

func NewSequencerNode(dbPath string, rpcPort string, p2pPort string, mode SequencerMode, privateKey string, bootstrapPeersStr string) (*SequencerNode) {
	// TODO: use sync=FULL for database durability during power loss.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		panic(fmt.Errorf("couldn't open database %s: %s", dbPath, err))
	}

	err = db.Ping()
	if err != nil {
		panic(fmt.Errorf("couldn't connect to database: %s", err))
	}

	seq := NewSequencerCore(db)

	// RPC.
	rpcAddr := fmt.Sprintf("0.0.0.0:%s", rpcPort)
	rpc := NewRPCNode(rpcAddr, seq)
	
	// P2P.
	p2pAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", p2pPort)
	bootstrapPeers, err := StringsToAddrs(strings.Split(bootstrapPeersStr, ","))
	if err != nil {
		panic(fmt.Errorf("couldn't parse bootstrap peers: %s", err))
	}

	p2p, err := NewP2PNode(p2pAddr, privateKey, bootstrapPeers)
	if err != nil {
		panic(fmt.Errorf("couldn't create network node: %s", err))
	}

	node := SequencerNode{
		Seq: seq,
		P2P: p2p,
		RPC: rpc,
		Mode: mode,
	}
	
	return &node
}

func (n *SequencerNode) Start() {
	// Hook them up.
	if n.Mode == PrimaryMode {
		n.Seq.OnNewBlock(func (block Block) {
			n.P2P.GossipNewBlock(block)
		})
	}

	if n.Mode == ReplicaMode {
		receiveBlockChan := make(chan Block)
		go n.P2P.ListenForNewBlocks(receiveBlockChan)
		go (func(){
			for {
				block := <-receiveBlockChan
				
				fmt.Println("verifying block:", block)
				err := n.Seq.ProcessBlock(block)
				if err != nil {
					fmt.Println("error while verifying block", block, ":", err)
				}
				fmt.Println("verification success for block:", block)
			}
		})()
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		go func() {
			defer wg.Done()
			n.P2P.Start()
		}()
		go func() {
			defer wg.Done()
			n.RPC.Start()
		}()
	}()
	wg.Wait()
}

func (n *SequencerNode) Close() {
	if err := n.P2P.Close(); err != nil {
		panic(err)
	}
}