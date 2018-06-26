package protocol

/*
Struct holds the messages that will be sent around in the protocol. You have
to define each message twice: once the actual message, and a second time
with the `*onet.TreeNode` embedded. The latter is used in the handler-function
so that it can find out who sent the message.
*/

import (
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/onet.v1/network"
	"time"
)

// ProtocolName can be used from other packages to refer to this protocol.
const ProtocolName = "BFTCosiProtocol"
const subProtocolName = "SubCoSi"

const DefaultProtocolTimeout = network.WaitRetry * time.Duration(network.MaxRetryConnect*2) * time.Millisecond
const DefaultSubleaderTimeout = time.Duration(float64(DefaultProtocolTimeout) * 0.01)
const DefaultLeavesTimeout = time.Duration(float64(DefaultProtocolTimeout) * 0.005)

type Announcement struct {
	 Proposal			[]byte
	 Publics			[]abstract.Point
	 SubleaderTimeout	time.Duration
	 LeafTimeout		time.Duration
}

// StructAnnouncement just contains Announcement and the data necessary to identify and
// process the message in the sda framework.
type StructAnnouncement struct {
	*onet.TreeNode //sender
	Announcement
}

type Commitment struct {
	CoSiCommitment abstract.Point
	Mask           []byte
}

// StructCommitment just contains Commitment and the data necessary to identify and
// process the message in the sda framework.
type StructCommitment struct {
	*onet.TreeNode
	Commitment
}

type Challenge struct {
	CoSiChallenge abstract.Scalar
}

// StructChallenge just contains Challenge and the data necessary to identify and
// process the message in the sda framework.
type StructChallenge struct {
	*onet.TreeNode
	Challenge
}

type Response struct {
	CoSiReponse abstract.Scalar
}

// StructResponse just contains Response and the data necessary to identify and
// process the message in the sda framework.
type StructResponse struct {
	*onet.TreeNode
	Response
}

type Stop struct {}

type StructStop struct {
	*onet.TreeNode
	Stop
}
