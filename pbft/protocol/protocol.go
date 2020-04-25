package protocol


import (
	"errors"
	"sync"
	"time"
	"bytes"
	"math"
	"fmt"
	"encoding/json"

	"github.com/csanti/onet"
	"github.com/csanti/onet/log"
	"github.com/csanti/onet/network"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/sign/schnorr"


	"crypto/sha512"
)

func init() {
	log.SetDebugVisible(1)
	network.RegisterMessages(PrePrepare{}, Prepare{}, Commit{}, Reply{})
	onet.GlobalProtocolRegister(DefaultProtocolName, NewProtocol)
}


type VerificationFn func(msg []byte, data []byte) bool

var defaultTimeout = 60 * time.Second

type PbftProtocol struct {
	*onet.TreeNodeInstance

	Msg					[]byte
	Data 				[]byte
	nNodes				int

	FinalReply 			chan []byte
	startChan       	chan bool
	stoppedOnce    		sync.Once
	verificationFn  	VerificationFn
	Timeout 			time.Duration
	PubKeysMap			map[string]kyber.Point

	ChannelPrePrepare   chan StructPrePrepare
	ChannelPrepare 		chan StructPrepare
	ChannelCommit		chan StructCommit
	ChannelReply		chan StructReply

}

// Check that *PbftProtocol implements onet.ProtocolInstance
var _ onet.ProtocolInstance = (*PbftProtocol)(nil)

// NewProtocol initialises the structure for use in one round
func NewProtocol(n *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {

	pubKeysMap := make(map[string]kyber.Point)
	for _, node := range n.Tree().List() {
		//fmt.Println(node.ServerIdentity, node.ServerIdentity.Public, node.ServerIdentity.ID.String())
		pubKeysMap[node.ServerIdentity.ID.String()] = node.ServerIdentity.Public
	}

	vf := func(msg, data []byte) bool {
		// Simulate verification function by sleeping
		b, _ := json.Marshal(msg)
		m := time.Duration(len(b) / (500 * 1024))  //verification of 150ms per 500KB simulated
		waitTime := 150 * time.Millisecond * m
		log.Lvl3("Verifying for", waitTime)
		time.Sleep(waitTime)  

		return true 
	}

	t := &PbftProtocol{
		TreeNodeInstance: 	n,
		nNodes: 			n.Tree().Size(),
		startChan:       	make(chan bool, 1),
		FinalReply:   		make(chan []byte, 1),
		PubKeysMap:			pubKeysMap,
		Data:            	make([]byte, 0),
		verificationFn:		vf,
	}

	for _, channel := range []interface{}{
		&t.ChannelPrePrepare,
		&t.ChannelPrepare,
		&t.ChannelCommit,
		&t.ChannelReply,
	} {
		err := t.RegisterChannel(channel)
		if err != nil {
			return nil, errors.New("couldn't register channel: " + err.Error())
		}
	}

	return t, nil
}

// Start sends the Announce-message to all children
func (pbft *PbftProtocol) Start() error {
	// TODO verify args not null

	log.Lvl3("Starting PbftProtocol")
	
	return nil
}

func (pbft *PbftProtocol) Dispatch() error {

	log.Lvl3(pbft.ServerIdentity(), "Started node")

	nRepliesThreshold := int(math.Ceil(float64(pbft.nNodes - 1 ) * (float64(2)/float64(3)))) + 1
	nRepliesThreshold = min(nRepliesThreshold, pbft.nNodes - 1)

	// Verification of the data
	verifyChan := make(chan bool, 1)

	var futureDigest []byte
	if pbft.IsRoot() {

		// send pre-prepare phase
		digest := sha512.Sum512(pbft.Msg) // TODO digest is correct?

		sig, err := schnorr.Sign(pbft.Suite(), pbft.Private(), pbft.Msg)
		if err != nil {
			return err
		}

		go func() {
			if errs := pbft.SendToChildrenInParallel(&PrePrepare{Msg:pbft.Msg, Digest:digest[:], Sig:sig, Sender:pbft.ServerIdentity().ID.String()}); len(errs) > 0 {
				log.Lvl3(pbft.ServerIdentity(), "failed to send pre-prepare to all children")
			}
		}()

		futureDigest = digest[:]

	} else {
		// wait for pre-prepare message from leader
		log.Lvl3(pbft.ServerIdentity(), "Waiting for preprepare")
		preprepare, channelOpen := <-pbft.ChannelPrePrepare
		if !channelOpen {
			return nil
		}
		log.Lvl3(pbft.ServerIdentity(), "Received PrePrepare. Verifying...")
		go func() {
			verifyChan <- pbft.verificationFn(preprepare.Msg, pbft.Data)
		}()

		// Verify the signature for authentication
		err := schnorr.Verify(pbft.Suite(), pbft.PubKeysMap[preprepare.Sender], preprepare.Msg, preprepare.Sig)
		if err != nil {
			return err
		}

		// verify message digest
		digest := sha512.Sum512(preprepare.Msg)
		if !bytes.Equal(digest[:], preprepare.Digest) {
			log.Lvl3(pbft.ServerIdentity(), "received pre-prepare digest is not correct")
		}

		futureDigest = preprepare.Digest

		ok := <-verifyChan
		if !ok {
			return fmt.Errorf("verification failed on node")
		}
	}

	// Sign message and broadcast
	signedDigest, err := schnorr.Sign(pbft.Suite(), pbft.Private(), futureDigest)
	if err != nil {
		return err
	}
	
	// broadcast Prepare message to all nodes
	if errs := pbft.Broadcast(&Prepare{Digest:futureDigest, Sig:signedDigest, Sender:pbft.ServerIdentity().ID.String()}); len(errs) > 0 {
		log.Lvl3(pbft.ServerIdentity(), "error while broadcasting prepare message")
	}

	t := time.After(defaultTimeout * 2)
	nReceivedPrepareMessages := 0
	
loop:
	for  i := 0; i <= nRepliesThreshold - 1; i++  {
		select {
		case prepare, channelOpen := <-pbft.ChannelPrepare:
			if !channelOpen {
				return nil
			}
			// Verify the signature for authentication
			err := schnorr.Verify(pbft.Suite(), pbft.PubKeysMap[prepare.Sender], prepare.Digest, prepare.Sig)
			if err != nil {
				return err
			}

			nReceivedPrepareMessages++
		case <-t:
			// TODO
			break loop
		}	
	}

	if !(nReceivedPrepareMessages >= nRepliesThreshold) {
		errors.New("node didn't receive enough prepare messages. Stopping.")
	} else {
		log.Lvl2(pbft.ServerIdentity(), "Received enough prepare messages (> 2/3 + 1):", nReceivedPrepareMessages, "/", pbft.nNodes)
	}

	//digest := sha512.Sum512(pbft.Msg)

	// Sign message and broadcast
	signedDigest2, err := schnorr.Sign(pbft.Suite(), pbft.Private(), futureDigest)
	if err != nil {
		return err
	}

	// Broadcast commit message
	if errs := pbft.Broadcast(&Commit{Digest:futureDigest, Sender:pbft.ServerIdentity().ID.String(), Sig:signedDigest2}); len(errs) > 0 {
		log.Lvl1(pbft.ServerIdentity(), "error while broadcasting commit message")
	}

	nReceivedCommitMessages := 0

commitLoop:
	for  i := 0; i <= nRepliesThreshold - 1; i++  {
		select {
		case commit, channelOpen := <-pbft.ChannelCommit:
			if !channelOpen {
				return nil
			}

			// Verify the signature for authentication
			err := schnorr.Verify(pbft.Suite(), pbft.PubKeysMap[commit.Sender], commit.Digest, commit.Sig)
			if err != nil {
				return err
			}
			nReceivedCommitMessages++
		case <-t:
			// TODO
			break commitLoop
		}
	}

	if !(nReceivedCommitMessages >= nRepliesThreshold) {
		log.Lvl1(pbft.ServerIdentity(), "node didn't receive enough commit messages. Stopping.", nReceivedCommitMessages, " / ", nRepliesThreshold)
		return errors.New("node didn't receive enough commit messages. Stopping.")
	} else {
		log.Lvl1(pbft.ServerIdentity(), "Received enough commit messages (> 2/3 + 1):", nReceivedCommitMessages, "/", pbft.nNodes)
	}

	receivedReplies := 0

	if pbft.IsRoot() {
replyLoop:
		for  i := 0; i <= nRepliesThreshold - 1; i++  {
			select {
			case reply, channelOpen := <-pbft.ChannelReply:
				if !channelOpen {
					return nil
				}

				// Verify the signature for authentication
				err := schnorr.Verify(pbft.Suite(), pbft.PubKeysMap[reply.Sender], reply.Result, reply.Sig)
				if err != nil {
					return err
				}

				receivedReplies++
				log.Lvl2("Leader got one reply, total received is now", receivedReplies, "out of", nRepliesThreshold, "needed.")
				
			case <-time.After(defaultTimeout * 2):
				// wait a bit longer than the protocol timeout
				log.Lvl3("didn't get reply in time")
				break replyLoop
			}
		}

		pbft.FinalReply <- futureDigest[:]

	} else {
		err := pbft.SendToParent(&Reply{Result:futureDigest, Sender:pbft.ServerIdentity().ID.String(), Sig:signedDigest})
		if err != nil {
			return err
		}
	}

	return nil
}

// Shutdown stops the protocol
func (pbft *PbftProtocol) Shutdown() error {
	pbft.stoppedOnce.Do(func() {
		close(pbft.ChannelPrePrepare)
		close(pbft.ChannelPrepare)
		close(pbft.ChannelCommit)
		close(pbft.ChannelReply)
	})
	return nil
}


func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}