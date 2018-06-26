package protocol

import (
	"time"

	"bls-ftcosi/bftcosi/cosi"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"fmt"
	"errors"
)

// CoSiSubProtocolNode holds the different channels used to receive the different protocol messages.
type CoSiSubProtocolNode struct {
	*onet.TreeNodeInstance
	Publics          []abstract.Point
	Proposal         []byte
	SubleaderTimeout time.Duration
	LeavesTimeout    time.Duration
	hasStopped       bool //used since Shutdown can be called multiple time

	//protocol/subprotocol channels
	subleaderNotResponding chan bool
	subCommitment		   chan StructCommitment
	subResponse            chan StructResponse

	//internodes channels
	ChannelAnnouncement    chan StructAnnouncement
	ChannelCommitment      chan StructCommitment
	ChannelChallenge       chan StructChallenge
	ChannelResponse        chan StructResponse
}

// The `NewSubProtocol` method is used to define the subprotocol and to register
// the channels where the messages will be received.
func NewSubProtocol(n *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {

	c := &CoSiSubProtocolNode{
		TreeNodeInstance:       n,
		hasStopped:				false,
	}

	if n.IsRoot() {
		c.subleaderNotResponding = make(chan bool)
		c.subCommitment	= make(chan StructCommitment)
		c.subResponse =	make(chan StructResponse)
	}

	for _, channel := range []interface{}{&c.ChannelAnnouncement, &c.ChannelCommitment, &c.ChannelChallenge, &c.ChannelResponse} {
		err := c.RegisterChannel(channel)
		if err != nil {
			return nil, errors.New("couldn't register channel: " + err.Error())
		}
	}
	err := c.RegisterHandler(c.HandleStop)
	if err != nil {
		return nil, errors.New("couldn't register stop handler: " + err.Error())
	}
	return c, nil
}

func (p *CoSiSubProtocolNode) Shutdown() error {
	if !p.hasStopped {
		close(p.ChannelAnnouncement)
		close(p.ChannelCommitment)
		close(p.ChannelChallenge)
		close(p.ChannelResponse)
		p.hasStopped = true
	}
	return nil
}

//Dispatch() is the main method of the subprotocol, running on each node and handling the messages in order
func (p *CoSiSubProtocolNode) Dispatch() error {

	// ----- Announcement -----
	announcement, channelOpen := <-p.ChannelAnnouncement
	if !channelOpen {
		return nil
	}
	log.Lvl3(p.ServerIdentity().Address, "received announcement")
	p.Publics = announcement.Publics
	p.SubleaderTimeout = announcement.SubleaderTimeout
	p.LeavesTimeout = announcement.LeafTimeout

	err := p.SendToChildren(&announcement.Announcement)
	if err != nil {
		return err
	}

	// ----- Commitment -----
	commitments := make([]StructCommitment, 0)
	if p.IsRoot() {
		select { //one commitment expected
		case commitment, channelOpen:= <-p.ChannelCommitment:
			if !channelOpen {
				return nil
			}
			commitments = append(commitments, commitment)
		case <-time.After(p.SubleaderTimeout):
			p.subleaderNotResponding <- true
			return nil
		}
	} else {
		t := time.After(p.LeavesTimeout)
		loop:
		for i:=0 ; i<len(p.Children()) ; i++ {
			select {
			case commitment, channelOpen := <-p.ChannelCommitment:
				if !channelOpen {
					return nil
				}
				commitments = append(commitments, commitment)
			case <-t:
				break loop
			}
		}
	}

	committedChildren := make([]*onet.TreeNode, 0)
	for _, commitment := range commitments {
		if commitment.TreeNode.Parent != p.TreeNode() {
			return errors.New("received a Commitment from a non-Children node")
		}
		committedChildren = append(committedChildren, commitment.TreeNode)
	}
	log.Lvl3(p.ServerIdentity().Address, "finished receiving commitments, ", len(commitments), "commitment(s) received")

 	var secret abstract.Scalar

 	// if root, send commitment to super-protocol
	if p.IsRoot() {
		if len(commitments) != 1 {
			return fmt.Errorf("root node in subprotocol should have received 1 commitment," +
				"but received %d", len(commitments))
		}
		p.subCommitment <- commitments[0]

	// if not root, compute personal commitment and send to parent
	} else {
		var commitment abstract.Point
		var mask *cosi.Mask
		secret, commitment, mask, err = generateCommitmentAndAggregate(p.TreeNodeInstance, p.Publics, commitments)
		if err != nil {
			return err
		}
		err = p.SendToParent(&Commitment{commitment, mask.Mask()})
		if err != nil {
			return err
		}
	}

	// ----- Challenge -----
	challenge, channelOpen := <-p.ChannelChallenge
	if !channelOpen {
		return nil
	}
	log.Lvl3(p.ServerIdentity().Address, "received challenge")
	for _, TreeNode := range committedChildren {
		err = p.SendTo(TreeNode, &challenge.Challenge)
		if err != nil {
			return err
		}
	}

	// ----- Response -----

	//get response
	if p.IsLeaf() {
		p.ChannelResponse <- StructResponse{}
	}
	responses := make([]StructResponse, 0)

	for  i:=0;i<len(committedChildren);i++ {
		response, channelOpen := <-p.ChannelResponse
		if !channelOpen {
			return nil
		}
		responses = append(responses, response)
	}
	log.Lvl3(p.ServerIdentity().Address, "received all", len(responses),"response(s)")

	//if root, send response to super-protocol
	if p.IsRoot() {
		if len(responses) != 1 {
			return fmt.Errorf("root node in subprotocol should have received 1 response," +
				"but received %d", len(commitments))
		}
		p.subResponse <- responses[0]

	// if not root, generate own response and send to parent
	} else {
		response, err := generateResponse(p.TreeNodeInstance, responses, secret, challenge.Challenge.CoSiChallenge)
		if err != nil {
			return err
		}
		err = p.SendToParent(&Response{response})
		if err != nil {
			return err
		}
	}

	//TODO: see if should stop node or be ready for another proposal
	return nil
}

//HandleStop is called when a Stop message is send to this node.
// It broadcasts the message and stops the node
func (p *CoSiSubProtocolNode) HandleStop(stop StructStop) error {
	defer p.Done()
	if p.IsRoot() {
		p.Broadcast(&stop.Stop)
	}
	return nil
}

// Start is done only by root and starts the subprotocol
func (p *CoSiSubProtocolNode) Start() error {
	log.Lvl3("Starting subCoSi")
	if p.Proposal == nil {
		return fmt.Errorf("subprotocol started without any proposal set")
	} else if p.Publics == nil || len(p.Publics) < 1 {
		return fmt.Errorf("subprotocol started with an invlid public key list")
	}
	if p.SubleaderTimeout < 1 {
		p.SubleaderTimeout = DefaultSubleaderTimeout
	}
	if p.LeavesTimeout < 1 {
		p.LeavesTimeout = DefaultLeavesTimeout
	}

	announcement := StructAnnouncement{p.TreeNode(),
		Announcement{p.Proposal, p.Publics,
		p.SubleaderTimeout, p.LeavesTimeout}}
	p.ChannelAnnouncement <- announcement
	return nil
}
