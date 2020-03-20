package mock

// ListIndexUpdaterStub -
type ListIndexUpdaterStub struct {
	UpdateListAndIndexCalled func(pubKey string, list string, index int32) error
}

// UpdateListAndIndex -
func (lius *ListIndexUpdaterStub) UpdateListAndIndex(pubKey string, shardID uint32, list string, index int32) error {
	if lius.UpdateListAndIndexCalled != nil {
		return lius.UpdateListAndIndexCalled(pubKey, list, index)
	}

	return nil
}

// IsInterfaceNil returns true if there is no value under the interface
func (lius *ListIndexUpdaterStub) IsInterfaceNil() bool {
	return lius == nil
}