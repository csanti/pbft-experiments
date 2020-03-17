package sda

import (
	"testing"

	"github.com/csanti/pbft-experiments/cothority/log"
)

// To avoid setting up testing-verbosity in all tests
func TestMain(m *testing.M) {
	log.MainTest(m)
}
