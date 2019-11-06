package external

import (
	"github.com/ElrondNetwork/elrond-go/process/smartContract"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
)

// TODO: Move interface to scDataGetter.go
// TODO: Rename ScDataGetter to "SmartContractRunSimulator"

// ScDataGetter defines how data should be get from a SC account
type ScDataGetter interface {
	RunAndGetVMOutput(command *smartContract.CommandRunFunction) (*vmcommon.VMOutput, error)
	IsInterfaceNil() bool
}

// StatusMetricsHandler is the interface that defines what a node details handler/provider should do
type StatusMetricsHandler interface {
	StatusMetricsMap() (map[string]interface{}, error)
	IsInterfaceNil() bool
}
