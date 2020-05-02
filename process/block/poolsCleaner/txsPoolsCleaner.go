package poolsCleaner

import (
	"bytes"
	"sync"
	"time"

	"github.com/ElrondNetwork/elrond-go/core"
	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/data"
	"github.com/ElrondNetwork/elrond-go/data/state"
	"github.com/ElrondNetwork/elrond-go/dataRetriever"
	"github.com/ElrondNetwork/elrond-go/process"
	"github.com/ElrondNetwork/elrond-go/sharding"
	"github.com/ElrondNetwork/elrond-go/storage"
	"github.com/ElrondNetwork/elrond-go/storage/txcache"
)

// sleepTime defines the time between each iteration made in clean...Pools methods
const sleepTime = time.Minute

const (
	blockTx = iota
	rewardTx
	unsignedTx
)

type txInfo struct {
	round           int64
	senderShardID   uint32
	receiverShardID uint32
	txType          int8
	txStore         storage.Cacher
}

// txsPoolsCleaner represents a pools cleaner that checks and cleans txs which should not be in pool anymore
type txsPoolsCleaner struct {
	addressPubkeyConverter   state.PubkeyConverter
	blockTransactionsPool    dataRetriever.ShardedDataCacherNotifier
	rewardTransactionsPool   dataRetriever.ShardedDataCacherNotifier
	unsignedTransactionsPool dataRetriever.ShardedDataCacherNotifier
	rounder                  process.Rounder
	shardCoordinator         sharding.Coordinator

	mutMapTxsRounds sync.RWMutex
	mapTxsRounds    map[string]*txInfo
	emptyAddress    []byte
}

// NewTxsPoolsCleaner will return a new txs pools cleaner
func NewTxsPoolsCleaner(
	addressPubkeyConverter state.PubkeyConverter,
	dataPool dataRetriever.PoolsHolder,
	rounder process.Rounder,
	shardCoordinator sharding.Coordinator,
) (*txsPoolsCleaner, error) {

	if check.IfNil(addressPubkeyConverter) {
		return nil, process.ErrNilPubkeyConverter
	}
	if check.IfNil(dataPool) {
		return nil, process.ErrNilPoolsHolder
	}
	if check.IfNil(dataPool.Transactions()) {
		return nil, process.ErrNilTransactionPool
	}
	if check.IfNil(dataPool.RewardTransactions()) {
		return nil, process.ErrNilRewardTxDataPool
	}
	if check.IfNil(dataPool.UnsignedTransactions()) {
		return nil, process.ErrNilUnsignedTxDataPool
	}
	if check.IfNil(rounder) {
		return nil, process.ErrNilRounder
	}
	if check.IfNil(shardCoordinator) {
		return nil, process.ErrNilShardCoordinator
	}

	tpc := txsPoolsCleaner{
		addressPubkeyConverter:   addressPubkeyConverter,
		blockTransactionsPool:    dataPool.Transactions(),
		rewardTransactionsPool:   dataPool.RewardTransactions(),
		unsignedTransactionsPool: dataPool.UnsignedTransactions(),
		rounder:                  rounder,
		shardCoordinator:         shardCoordinator,
	}

	tpc.mapTxsRounds = make(map[string]*txInfo)

	tpc.blockTransactionsPool.RegisterHandler(tpc.receivedBlockTx)
	tpc.rewardTransactionsPool.RegisterHandler(tpc.receivedRewardTx)
	tpc.unsignedTransactionsPool.RegisterHandler(tpc.receivedUnsignedTx)

	tpc.emptyAddress = make([]byte, tpc.addressPubkeyConverter.Len())

	go tpc.cleanTxsPools()

	return &tpc, nil
}

func (tpc *txsPoolsCleaner) cleanTxsPools() {
	for {
		time.Sleep(sleepTime)
		numTxsInMap := tpc.cleanTxsPoolsIfNeeded()
		log.Debug("txsPoolsCleaner.cleanTxsPools", "num txs in map", numTxsInMap)
	}
}

func (tpc *txsPoolsCleaner) receivedBlockTx(key []byte, value interface{}) {
	if key == nil {
		return
	}

	log.Trace("txsPoolsCleaner.receivedBlockTx", "hash", key)

	wrappedTx, ok := value.(*txcache.WrappedTransaction)
	if !ok {
		log.Warn("txsPoolsCleaner.receivedBlockTx", "error", process.ErrWrongTypeAssertion)
		return
	}

	tpc.processReceivedTx(key, wrappedTx.SenderShardID, wrappedTx.ReceiverShardID, blockTx)
}

func (tpc *txsPoolsCleaner) receivedRewardTx(key []byte, _ interface{}) {
	if key == nil {
		return
	}

	log.Trace("txsPoolsCleaner.receivedRewardTx", "hash", key)

	senderShardID := core.MetachainShardId
	receiverShardID := tpc.shardCoordinator.SelfId()
	tpc.processReceivedTx(key, senderShardID, receiverShardID, rewardTx)
}

func (tpc *txsPoolsCleaner) receivedUnsignedTx(key []byte, value interface{}) {
	if key == nil {
		return
	}

	log.Trace("txsPoolsCleaner.receivedUnsignedTx", "hash", key)

	tx, ok := value.(data.TransactionHandler)
	if !ok {
		log.Warn("txsPoolsCleaner.receivedUnsignedTx", "error", process.ErrWrongTypeAssertion)
		return
	}

	senderShardID, receiverShardID, err := tpc.computeSenderAndReceiverShards(tx)
	if err != nil {
		log.Debug("txsPoolsCleaner.receivedUnsignedTx", "error", err.Error())
		return
	}

	tpc.processReceivedTx(key, senderShardID, receiverShardID, unsignedTx)
}

func (tpc *txsPoolsCleaner) processReceivedTx(
	key []byte,
	senderShardID uint32,
	receiverShardID uint32,
	txType int8,
) {
	tpc.mutMapTxsRounds.Lock()
	defer tpc.mutMapTxsRounds.Unlock()

	if _, ok := tpc.mapTxsRounds[string(key)]; !ok {
		transactionPool := tpc.getTransactionPool(txType)
		if transactionPool == nil {
			return
		}

		strCache := process.ShardCacherIdentifier(senderShardID, receiverShardID)
		txStore := transactionPool.ShardDataStore(strCache)
		if txStore == nil {
			return
		}

		currTxInfo := &txInfo{
			round:           tpc.rounder.Index(),
			senderShardID:   senderShardID,
			receiverShardID: receiverShardID,
			txType:          txType,
			txStore:         txStore,
		}

		tpc.mapTxsRounds[string(key)] = currTxInfo

		log.Trace("transaction has been added",
			"hash", key,
			"round", currTxInfo.round,
			"sender", currTxInfo.senderShardID,
			"receiver", currTxInfo.receiverShardID,
			"type", getTxTypeName(currTxInfo.txType))
	}
}

func (tpc *txsPoolsCleaner) cleanTxsPoolsIfNeeded() int {
	tpc.mutMapTxsRounds.Lock()
	defer tpc.mutMapTxsRounds.Unlock()

	numTxsCleaned := 0

	for hash, currTxInfo := range tpc.mapTxsRounds {
		_, ok := currTxInfo.txStore.Get([]byte(hash))
		if !ok {
			log.Trace("transaction not found in pool",
				"hash", []byte(hash),
				"round", currTxInfo.round,
				"sender", currTxInfo.senderShardID,
				"receiver", currTxInfo.receiverShardID,
				"type", getTxTypeName(currTxInfo.txType))
			delete(tpc.mapTxsRounds, hash)
			continue
		}

		roundDif := tpc.rounder.Index() - currTxInfo.round
		if roundDif <= process.MaxRoundsToKeepUnprocessedTransactions {
			log.Trace("cleaning transaction not yet allowed",
				"hash", []byte(hash),
				"round", currTxInfo.round,
				"sender", currTxInfo.senderShardID,
				"receiver", currTxInfo.receiverShardID,
				"type", getTxTypeName(currTxInfo.txType),
				"round dif", roundDif)

			continue
		}

		currTxInfo.txStore.Remove([]byte(hash))
		delete(tpc.mapTxsRounds, hash)
		numTxsCleaned++

		log.Trace("transaction has been cleaned",
			"hash", []byte(hash),
			"round", currTxInfo.round,
			"sender", currTxInfo.senderShardID,
			"receiver", currTxInfo.receiverShardID,
			"type", getTxTypeName(currTxInfo.txType))
	}

	if numTxsCleaned > 0 {
		log.Debug("txsPoolsCleaner.cleanTxsPoolsIfNeeded", "num txs cleaned", numTxsCleaned)
	}

	return len(tpc.mapTxsRounds)
}

func (tpc *txsPoolsCleaner) getTransactionPool(txType int8) dataRetriever.ShardedDataCacherNotifier {
	switch txType {
	case blockTx:
		return tpc.blockTransactionsPool
	case rewardTx:
		return tpc.rewardTransactionsPool
	case unsignedTx:
		return tpc.unsignedTransactionsPool
	}

	return nil
}

func getTxTypeName(txType int8) string {
	switch txType {
	case blockTx:
		return "blockTx"
	case rewardTx:
		return "rewardTx"
	case unsignedTx:
		return "unsignedTx"
	}

	return "unknownTx"
}

func (tpc *txsPoolsCleaner) computeSenderAndReceiverShards(tx data.TransactionHandler) (uint32, uint32, error) {
	senderShardID, err := tpc.getShardFromAddress(tx.GetSndAddr())
	if err != nil {
		return 0, 0, err
	}

	receiverShardID, err := tpc.getShardFromAddress(tx.GetRcvAddr())
	if err != nil {
		return 0, 0, err
	}

	return senderShardID, receiverShardID, nil
}

func (tpc *txsPoolsCleaner) getShardFromAddress(address []byte) (uint32, error) {
	isEmptyAddress := bytes.Equal(address, tpc.emptyAddress)
	if isEmptyAddress {
		return tpc.shardCoordinator.SelfId(), nil
	}

	return tpc.shardCoordinator.ComputeId(address), nil
}