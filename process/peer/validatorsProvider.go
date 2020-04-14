package peer

import (
	"sync"

	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/data/state"
	"github.com/ElrondNetwork/elrond-go/process"
)

// validatorsProvider is the main interface for validators' provider
type validatorsProvider struct {
	mutCachedMap        sync.Mutex
	cachedMap           map[string]*state.ValidatorApiResponse
	validatorStatistics process.ValidatorStatisticsProcessor
	maxRating           uint32
	pubkeyConverter     state.PubkeyConverter
}

// NewValidatorsProvider instantiates a new validatorsProvider structure responsible of keeping account of
//  the latest information about the validators
func NewValidatorsProvider(
	validatorStatisticsProcessor process.ValidatorStatisticsProcessor,
	maxRating uint32,
	pubkeyConverter state.PubkeyConverter,
) (*validatorsProvider, error) {
	if check.IfNil(validatorStatisticsProcessor) {
		return nil, process.ErrNilValidatorStatistics
	}
	if check.IfNil(pubkeyConverter) {
		return nil, process.ErrNilPubkeyConverter
	}
	if maxRating == 0 {
		return nil, process.ErrMaxRatingZero
	}

	validatorsProvider := &validatorsProvider{
		mutCachedMap:        sync.Mutex{},
		cachedMap:           make(map[string]*state.ValidatorApiResponse),
		maxRating:           maxRating,
		validatorStatistics: validatorStatisticsProcessor,
		pubkeyConverter:     pubkeyConverter,
	}

	return validatorsProvider, nil
}

// GetLatestValidators gets the latest configuration of validators from the peerAccountsTrie
func (vp *validatorsProvider) GetLatestValidators() map[string]*state.ValidatorApiResponse {
	vp.mutCachedMap.Lock()
	defer vp.mutCachedMap.Unlock()

	latestHash, err := vp.validatorStatistics.RootHash()
	if err != nil {
		return vp.cachedMap
	}

	validators, err := vp.validatorStatistics.GetValidatorInfoForRootHash(latestHash)
	if err != nil {
		return vp.cachedMap
	}

	mapToReturn := make(map[string]*state.ValidatorApiResponse)
	for _, validatorInfosInShard := range validators {
		for _, validatorInfo := range validatorInfosInShard {
			strKey := vp.pubkeyConverter.Encode(validatorInfo.PublicKey)
			mapToReturn[strKey] = &state.ValidatorApiResponse{
				NumLeaderSuccess:         validatorInfo.LeaderSuccess,
				NumLeaderFailure:         validatorInfo.LeaderFailure,
				NumValidatorSuccess:      validatorInfo.ValidatorSuccess,
				NumValidatorFailure:      validatorInfo.ValidatorFailure,
				TotalNumLeaderSuccess:    validatorInfo.TotalLeaderSuccess,
				TotalNumLeaderFailure:    validatorInfo.TotalLeaderFailure,
				TotalNumValidatorSuccess: validatorInfo.TotalValidatorSuccess,
				TotalNumValidatorFailure: validatorInfo.TotalValidatorFailure,
				Rating:                   float32(validatorInfo.Rating) * 100 / float32(vp.maxRating),
				TempRating:               float32(validatorInfo.TempRating) * 100 / float32(vp.maxRating),
			}
		}
	}

	vp.cachedMap = mapToReturn

	return mapToReturn
}

// IsInterfaceNil returns true if there is no value under the interface
func (vs *validatorsProvider) IsInterfaceNil() bool {
	return vs == nil
}
