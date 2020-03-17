package services

import (
	// Importing the services so they register their services to SDA
	// automatically when importing github.com/csanti/pbft-experiments/cothority/services
	_ "github.com/dedis/cosi/service"
	_ "github.com/csanti/pbft-experiments/cothority/services/byzcoin_ng"
	_ "github.com/csanti/pbft-experiments/cothority/services/guard"
	_ "github.com/csanti/pbft-experiments/cothority/services/identity"
	_ "github.com/csanti/pbft-experiments/cothority/services/skipchain"
	_ "github.com/csanti/pbft-experiments/cothority/services/status"
)
