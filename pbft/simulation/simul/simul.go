package main

import (
	// Service needs to be imported here to be instantiated.
	_ "github.com/csanti/pbft-experiments/pbft/simulation"
	"github.com/csanti/onet/simul"
)

func main() {
	simul.Start()
}
