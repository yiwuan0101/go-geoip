package common

import (
	"github.com/oschwald/maxminddb-golang"
	"sync"
	"time"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "dev"               // replaced via -ldflags -X with the git tag on release builds; bare local builds show "dev"

var (
	CityReader, AsnReader, CnReader *maxminddb.Reader
	Mu                              sync.Mutex
)
