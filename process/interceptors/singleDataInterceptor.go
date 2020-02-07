package interceptors

import (
	"sync"

	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/p2p"
	"github.com/ElrondNetwork/elrond-go/process"
)

// SingleDataInterceptor is used for intercepting packed multi data
type SingleDataInterceptor struct {
	factory          process.InterceptedDataFactory
	processor        process.InterceptorProcessor
	throttler        process.InterceptorThrottler
	antifloodHandler process.P2PAntifloodHandler
}

// NewSingleDataInterceptor hooks a new interceptor for single data
func NewSingleDataInterceptor(
	factory process.InterceptedDataFactory,
	processor process.InterceptorProcessor,
	throttler process.InterceptorThrottler,
	antifloodHandler process.P2PAntifloodHandler,
) (*SingleDataInterceptor, error) {

	if check.IfNil(factory) {
		return nil, process.ErrNilInterceptedDataFactory
	}
	if check.IfNil(processor) {
		return nil, process.ErrNilInterceptedDataProcessor
	}
	if check.IfNil(throttler) {
		return nil, process.ErrNilInterceptorThrottler
	}
	if check.IfNil(antifloodHandler) {
		return nil, process.ErrNilAntifloodHandler
	}

	singleDataIntercept := &SingleDataInterceptor{
		factory:          factory,
		processor:        processor,
		throttler:        throttler,
		antifloodHandler: antifloodHandler,
	}

	return singleDataIntercept, nil
}

// ProcessReceivedMessage is the callback func from the p2p.Messenger and will be called each time a new message was received
// (for the topic this validator was registered to)
func (sdi *SingleDataInterceptor) ProcessReceivedMessage(message p2p.MessageP2P, fromConnectedPeer p2p.PeerID) error {
	err := preProcessMesage(sdi.throttler, sdi.antifloodHandler, message, fromConnectedPeer)
	if err != nil {
		return err
	}

	interceptedData, err := sdi.factory.Create(message.Data())
	if err != nil {
		sdi.throttler.EndProcessing()
		return err
	}

	err = interceptedData.CheckValidity()
	if err != nil {
		sdi.throttler.EndProcessing()
		return err
	}

	if !interceptedData.IsForCurrentShard() {
		sdi.throttler.EndProcessing()
		log.Trace("intercepted data is for other shards",
			"pid", p2p.MessageOriginatorPid(message),
			"seq no", p2p.MessageOriginatorSeq(message),
		)
		return nil
	}

	wgProcess := &sync.WaitGroup{}
	wgProcess.Add(1)
	go func() {
		wgProcess.Wait()
		sdi.throttler.EndProcessing()
	}()

	go processInterceptedData(sdi.processor, interceptedData, wgProcess, message)

	return nil
}

// IsInterfaceNil returns true if there is no value under the interface
func (sdi *SingleDataInterceptor) IsInterfaceNil() bool {
	return sdi == nil
}
