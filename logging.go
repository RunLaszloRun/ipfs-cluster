package ipfscluster

import logging "github.com/ipfs/go-log"

var logger = logging.Logger("cluster")

var facilities = []string{
	"cluster",
	"restapi",
	"ipfshttp",
	"monitor",
	"mapstate",
	"consensus",
	"raft",
	"pintracker",
	"ascendalloc",
	"diskinfo",
	"apitypes",
}

// SetFacilityLogLevel sets the log level for a given module
func SetFacilityLogLevel(f, l string) {
	/*
		CRITICAL Level = iota
		ERROR
		WARNING
		NOTICE
		INFO
		DEBUG
	*/
	logging.SetLogLevel(f, l)
}
