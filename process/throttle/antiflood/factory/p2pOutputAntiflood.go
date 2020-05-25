package factory

import (
	"math"

	"github.com/ElrondNetwork/elrond-go/config"
	"github.com/ElrondNetwork/elrond-go/process"
	"github.com/ElrondNetwork/elrond-go/process/throttle/antiflood"
	"github.com/ElrondNetwork/elrond-go/process/throttle/antiflood/floodPreventers"
	storageFactory "github.com/ElrondNetwork/elrond-go/storage/factory"
	"github.com/ElrondNetwork/elrond-go/storage/storageUnit"
)

// NewP2POutputAntiFlood will return an instance of an output antiflood component based on the config
func NewP2POutputAntiFlood(mainConfig config.Config) (process.P2PAntifloodHandler, error) {
	if mainConfig.Antiflood.Enabled {
		return initP2POutputAntiFlood(mainConfig)
	}

	return &disabledAntiFlood{}, nil
}

func initP2POutputAntiFlood(mainConfig config.Config) (process.P2PAntifloodHandler, error) {
	cacheConfig := storageFactory.GetCacherFromConfig(mainConfig.Antiflood.Cache)
	antifloodCache, err := storageUnit.NewCache(cacheConfig.Type, cacheConfig.Size, cacheConfig.Shards)
	if err != nil {
		return nil, err
	}

	peerMaxMessagesPerSecond := mainConfig.Antiflood.PeerMaxOutput.MessagesPerInterval
	peerMaxTotalSizePerSecond := mainConfig.Antiflood.PeerMaxOutput.TotalSizePerInterval
	floodPreventer, err := floodPreventers.NewQuotaFloodPreventer(
		antifloodCache,
		make([]floodPreventers.QuotaStatusHandler, 0),
		peerMaxMessagesPerSecond,
		peerMaxTotalSizePerSecond,
		math.MaxUint32,
		math.MaxUint64,
	)
	if err != nil {
		return nil, err
	}

	topicFloodPreventer := floodPreventers.NewNilTopicFloodPreventer()
	startResettingTopicFloodPreventer(topicFloodPreventer, make([]config.TopicMaxMessagesConfig, 0), floodPreventer)

	return antiflood.NewP2PAntiflood(topicFloodPreventer, floodPreventer)
}
