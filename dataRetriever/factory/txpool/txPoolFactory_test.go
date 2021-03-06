package txpool

import (
	"testing"

	"github.com/ElrondNetwork/elrond-go/dataRetriever/txpool"
	"github.com/ElrondNetwork/elrond-go/storage/storageUnit"
	"github.com/stretchr/testify/require"
)

func TestCreateNewTxPool_ShardedData(t *testing.T) {
	config := storageUnit.CacheConfig{Type: storageUnit.FIFOShardedCache, Capacity: 100, SizeInBytes: 40960, Shards: 1}
	args := txpool.ArgShardedTxPool{Config: config, MinGasPrice: 200000000000, NumberOfShards: 1}

	txPool, err := CreateTxPool(args)
	require.Nil(t, err)
	require.NotNil(t, txPool)

	config = storageUnit.CacheConfig{Type: storageUnit.LRUCache, Capacity: 100, Shards: 1}
	args = txpool.ArgShardedTxPool{Config: config, MinGasPrice: 200000000000, NumberOfShards: 1}
	txPool, err = CreateTxPool(args)
	require.Nil(t, err)
	require.NotNil(t, txPool)
}

func TestCreateNewTxPool_ShardedTxPool(t *testing.T) {
	config := storageUnit.CacheConfig{Capacity: 100, SizePerSender: 1, SizeInBytes: 40960, SizeInBytesPerSender: 40960, Shards: 1}
	args := txpool.ArgShardedTxPool{Config: config, MinGasPrice: 200000000000, NumberOfShards: 1}

	txPool, err := CreateTxPool(args)
	require.Nil(t, err)
	require.NotNil(t, txPool)
}
