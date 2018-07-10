package network

import (
	"fmt"
	"sync"
	"testing"

	"strconv"

	"golang.org/x/net/context"
)

var SimplePacketType PacketTypeID

func init() {
	SimplePacketType = RegisterPacketType(SimplePacket{})
}

type SimplePacket struct {
	Name string
}

func TestSimple(t *testing.T) {
	client := NewTCPHost()
	clientName := "client"
	server := NewTCPHost()
	serverName := "server"

	done := make(chan bool)
	listenCB := make(chan bool)

	srvConMu := sync.Mutex{}
	cConMu := sync.Mutex{}

	go func() {
		err := server.Listen("localhost:0", func(c Conn) {
			listenCB <- true
			srvConMu.Lock()
			defer srvConMu.Unlock()
			nm, _ := c.Receive(context.TODO())
			if nm.MsgType != SimplePacketType {
				c.Close()
				t.Fatal("Packet received not conform")
			}
			simplePacket := nm.Msg.(SimplePacket)
			if simplePacket.Name != clientName {
				t.Fatal("Not the right name")
			}
			c.Send(context.TODO(), &SimplePacket{serverName})
			//c.Close()
		})
		if err != nil {
			t.Fatal("Couldn't listen:", err)
		}
		done <- true
	}()
	cConMu.Lock()
	conn, err := client.Open("localhost:" + strconv.Itoa(<-server.listeningPort))
	if err != nil {
		t.Fatal(err)
	}
	// wait for the listen callback to be called at least once:
	<-listenCB

	conn.Send(context.TODO(), &SimplePacket{clientName})
	nm, err := conn.Receive(context.TODO())
	cConMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if nm.MsgType != SimplePacketType {
		t.Fatal(fmt.Sprintf("Packet received non conform %+v", nm))
	}
	sp := nm.Msg.(SimplePacket)
	if sp.Name != serverName {
		t.Fatal("Name no right")
	}
	cConMu.Lock()
	if err := client.Close(); err != nil {
		t.Fatal("Couldn't close client connection")
	}
	cConMu.Unlock()

	srvConMu.Lock()
	defer srvConMu.Unlock()
	if err := server.Close(); err != nil {
		t.Fatal("Couldn't close server connection")
	}
	<-done
}
