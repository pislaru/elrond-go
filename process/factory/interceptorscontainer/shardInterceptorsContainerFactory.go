package interceptorscontainer

import (
	"github.com/ElrondNetwork/elrond-go/core"
	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/core/throttler"
	"github.com/ElrondNetwork/elrond-go/crypto"
	"github.com/ElrondNetwork/elrond-go/data/state"
	"github.com/ElrondNetwork/elrond-go/marshal"
	"github.com/ElrondNetwork/elrond-go/process"
	"github.com/ElrondNetwork/elrond-go/process/factory"
	"github.com/ElrondNetwork/elrond-go/process/factory/containers"
	interceptorFactory "github.com/ElrondNetwork/elrond-go/process/interceptors/factory"
)

var _ process.InterceptorsContainerFactory = (*shardInterceptorsContainerFactory)(nil)

// shardInterceptorsContainerFactory will handle the creation the interceptors container for shards
type shardInterceptorsContainerFactory struct {
	*baseInterceptorsContainerFactory
	keyGen        crypto.KeyGenerator
	singleSigner  crypto.SingleSigner
	addrConverter state.AddressConverter
}

// NewShardInterceptorsContainerFactory is responsible for creating a new interceptors factory object
func NewShardInterceptorsContainerFactory(
	args ShardInterceptorsContainerFactoryArgs,
) (*shardInterceptorsContainerFactory, error) {
	if args.SizeCheckDelta > 0 {
		args.Marshalizer = marshal.NewSizeCheckUnmarshalizer(args.Marshalizer, args.SizeCheckDelta)
	}
	err := checkBaseParams(
		args.ShardCoordinator,
		args.Accounts,
		args.Marshalizer,
		args.Hasher,
		args.Store,
		args.DataPool,
		args.Messenger,
		args.MultiSigner,
		args.NodesCoordinator,
		args.BlackList,
		args.AntifloodHandler,
	)
	if err != nil {
		return nil, err
	}

	if check.IfNil(args.KeyGen) {
		return nil, process.ErrNilKeyGen
	}
	if check.IfNil(args.SingleSigner) {
		return nil, process.ErrNilSingleSigner
	}
	if check.IfNil(args.AddrConverter) {
		return nil, process.ErrNilAddressConverter
	}
	if check.IfNil(args.TxFeeHandler) {
		return nil, process.ErrNilEconomicsFeeHandler
	}
	if check.IfNil(args.BlockSignKeyGen) {
		return nil, process.ErrNilKeyGen
	}
	if check.IfNil(args.BlockSingleSigner) {
		return nil, process.ErrNilSingleSigner
	}
	if check.IfNil(args.HeaderSigVerifier) {
		return nil, process.ErrNilHeaderSigVerifier
	}
	if len(args.ChainID) == 0 {
		return nil, process.ErrInvalidChainID
	}
	if check.IfNil(args.ValidityAttester) {
		return nil, process.ErrNilValidityAttester
	}
	if check.IfNil(args.EpochStartTrigger) {
		return nil, process.ErrNilEpochStartTrigger
	}

	argInterceptorFactory := &interceptorFactory.ArgInterceptedDataFactory{
		Marshalizer:       args.Marshalizer,
		Hasher:            args.Hasher,
		ShardCoordinator:  args.ShardCoordinator,
		MultiSigVerifier:  args.MultiSigner,
		NodesCoordinator:  args.NodesCoordinator,
		KeyGen:            args.KeyGen,
		BlockKeyGen:       args.BlockSignKeyGen,
		Signer:            args.SingleSigner,
		BlockSigner:       args.BlockSingleSigner,
		AddrConv:          args.AddrConverter,
		FeeHandler:        args.TxFeeHandler,
		HeaderSigVerifier: args.HeaderSigVerifier,
		ChainID:           args.ChainID,
		ValidityAttester:  args.ValidityAttester,
		EpochStartTrigger: args.EpochStartTrigger,
	}

	container := containers.NewInterceptorsContainer()
	base := &baseInterceptorsContainerFactory{
		container:              container,
		accounts:               args.Accounts,
		shardCoordinator:       args.ShardCoordinator,
		messenger:              args.Messenger,
		store:                  args.Store,
		marshalizer:            args.Marshalizer,
		hasher:                 args.Hasher,
		multiSigner:            args.MultiSigner,
		dataPool:               args.DataPool,
		nodesCoordinator:       args.NodesCoordinator,
		argInterceptorFactory:  argInterceptorFactory,
		blackList:              args.BlackList,
		maxTxNonceDeltaAllowed: args.MaxTxNonceDeltaAllowed,
		antifloodHandler:       args.AntifloodHandler,
	}

	icf := &shardInterceptorsContainerFactory{
		baseInterceptorsContainerFactory: base,
		keyGen:                           args.KeyGen,
		singleSigner:                     args.SingleSigner,
		addrConverter:                    args.AddrConverter,
	}

	icf.globalThrottler, err = throttler.NewNumGoRoutineThrottler(numGoRoutines)
	if err != nil {
		return nil, err
	}

	return icf, nil
}

// Create returns an interceptor container that will hold all interceptors in the system
func (sicf *shardInterceptorsContainerFactory) Create() (process.InterceptorsContainer, error) {
	err := sicf.generateTxInterceptors()
	if err != nil {
		return nil, err
	}

	err = sicf.generateUnsignedTxsInterceptorsForShard()
	if err != nil {
		return nil, err
	}

	err = sicf.generateRewardTxInterceptor()
	if err != nil {
		return nil, err
	}

	err = sicf.generateHeaderInterceptors()
	if err != nil {
		return nil, err
	}

	err = sicf.generateMiniBlocksInterceptors()
	if err != nil {
		return nil, err
	}

	err = sicf.generateMetachainHeaderInterceptors()
	if err != nil {
		return nil, err
	}

	err = sicf.generateTrieNodesInterceptors()
	if err != nil {
		return nil, err
	}

	return sicf.container, nil
}

//------- Unsigned transactions interceptors

func (sicf *shardInterceptorsContainerFactory) generateUnsignedTxsInterceptorsForShard() error {
	err := sicf.generateUnsignedTxsInterceptors()
	if err != nil {
		return err
	}

	shardC := sicf.shardCoordinator
	identifierTx := factory.UnsignedTransactionTopic + shardC.CommunicationIdentifier(core.MetachainShardId)

	interceptor, err := sicf.createOneUnsignedTxInterceptor(identifierTx)
	if err != nil {
		return err
	}

	return sicf.container.Add(identifierTx, interceptor)
}

func (sicf *shardInterceptorsContainerFactory) generateTrieNodesInterceptors() error {
	shardC := sicf.shardCoordinator

	keys := make([]string, 0)
	interceptorsSlice := make([]process.Interceptor, 0)

	identifierTrieNodes := factory.AccountTrieNodesTopic + shardC.CommunicationIdentifier(core.MetachainShardId)
	interceptor, err := sicf.createOneTrieNodesInterceptor(identifierTrieNodes)
	if err != nil {
		return err
	}

	keys = append(keys, identifierTrieNodes)
	interceptorsSlice = append(interceptorsSlice, interceptor)

	identifierTrieNodes = factory.ValidatorTrieNodesTopic + shardC.CommunicationIdentifier(core.MetachainShardId)
	interceptor, err = sicf.createOneTrieNodesInterceptor(identifierTrieNodes)
	if err != nil {
		return err
	}

	keys = append(keys, identifierTrieNodes)
	interceptorsSlice = append(interceptorsSlice, interceptor)

	return sicf.container.AddMultiple(keys, interceptorsSlice)
}

//------- Reward transactions interceptors

func (sicf *shardInterceptorsContainerFactory) generateRewardTxInterceptor() error {
	shardC := sicf.shardCoordinator

	keys := make([]string, 0)
	interceptorSlice := make([]process.Interceptor, 0)

	identifierTx := factory.RewardsTransactionTopic + shardC.CommunicationIdentifier(core.MetachainShardId)
	interceptor, err := sicf.createOneRewardTxInterceptor(identifierTx)
	if err != nil {
		return err
	}

	keys = append(keys, identifierTx)
	interceptorSlice = append(interceptorSlice, interceptor)

	return sicf.container.AddMultiple(keys, interceptorSlice)
}

// IsInterfaceNil returns true if there is no value under the interface
func (sicf *shardInterceptorsContainerFactory) IsInterfaceNil() bool {
	return sicf == nil
}