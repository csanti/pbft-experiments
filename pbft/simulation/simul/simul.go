package main

import (
	// Service needs to be imported here to be instantiated.
	_ "github.com/csanti/pbft-experiments/pbft/simulation"
	"go.dedis.ch/onet/simul"
)

func main() {
	simul.Start()
}
