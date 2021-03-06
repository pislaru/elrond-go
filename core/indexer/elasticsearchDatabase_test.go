package indexer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ElrondNetwork/elrond-go-logger"
	"github.com/ElrondNetwork/elrond-go/core"
	"github.com/ElrondNetwork/elrond-go/core/mock"
	"github.com/ElrondNetwork/elrond-go/data"
	dataBlock "github.com/ElrondNetwork/elrond-go/data/block"
	"github.com/ElrondNetwork/elrond-go/data/receipt"
	"github.com/ElrondNetwork/elrond-go/data/rewardTx"
	"github.com/ElrondNetwork/elrond-go/data/smartContractResult"
	"github.com/ElrondNetwork/elrond-go/data/transaction"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/stretchr/testify/require"
)

func newTestElasticSearchDatabase(elasticsearchWriter databaseWriterHandler, arguments elasticSearchDatabaseArgs) *elasticSearchDatabase {
	return &elasticSearchDatabase{
		txDatabaseProcessor: newTxDatabaseProcessor(
			arguments.hasher,
			arguments.marshalizer,
			arguments.addressPubkeyConverter,
			arguments.validatorPubkeyConverter,
		),
		dbWriter:    elasticsearchWriter,
		marshalizer: arguments.marshalizer,
		hasher:      arguments.hasher,
	}
}

func createMockElasticsearchDatabaseArgs() elasticSearchDatabaseArgs {
	return elasticSearchDatabaseArgs{
		addressPubkeyConverter:   mock.NewPubkeyConverterMock(32),
		validatorPubkeyConverter: mock.NewPubkeyConverterMock(32),
		url:                      "url",
		userName:                 "username",
		password:                 "password",
		hasher:                   &mock.HasherMock{},
		marshalizer:              &mock.MarshalizerMock{},
	}
}

func newTestTxPool() map[string]data.TransactionHandler {
	txPool := map[string]data.TransactionHandler{
		"tx1": &transaction.Transaction{
			Nonce:     uint64(1),
			Value:     big.NewInt(1),
			RcvAddr:   []byte("receiver_address1"),
			SndAddr:   []byte("sender_address1"),
			GasPrice:  uint64(10000),
			GasLimit:  uint64(1000),
			Data:      []byte("tx_data1"),
			Signature: []byte("signature1"),
		},
		"tx2": &transaction.Transaction{
			Nonce:     uint64(2),
			Value:     big.NewInt(2),
			RcvAddr:   []byte("receiver_address2"),
			SndAddr:   []byte("sender_address2"),
			GasPrice:  uint64(10000),
			GasLimit:  uint64(1000),
			Data:      []byte("tx_data2"),
			Signature: []byte("signature2"),
		},
		"tx3": &transaction.Transaction{
			Nonce:     uint64(3),
			Value:     big.NewInt(3),
			RcvAddr:   []byte("receiver_address3"),
			SndAddr:   []byte("sender_address3"),
			GasPrice:  uint64(10000),
			GasLimit:  uint64(1000),
			Data:      []byte("tx_data3"),
			Signature: []byte("signature3"),
		},
	}

	return txPool
}

func newTestBlockBody() *dataBlock.Body {
	return &dataBlock.Body{
		MiniBlocks: []*dataBlock.MiniBlock{
			{TxHashes: [][]byte{[]byte("tx1"), []byte("tx2")}, ReceiverShardID: 2, SenderShardID: 2},
			{TxHashes: [][]byte{[]byte("tx3")}, ReceiverShardID: 4, SenderShardID: 1},
		},
	}
}

func TestNewElasticSearchDatabase_IndexesError(t *testing.T) {
	indexes := []string{txIndex, blockIndex, tpsIndex, validatorsIndex, roundIndex}

	for _, index := range indexes {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == ("/" + index) {
				w.WriteHeader(http.StatusNotFound)
			}
		}))

		arguments := createMockElasticsearchDatabaseArgs()
		arguments.url = ts.URL

		elasticDatabase, err := newElasticSearchDatabase(arguments)
		require.Nil(t, elasticDatabase)
		require.Equal(t, ErrCannotCreateIndex, err)
	}
}

func TestElasticseachDatabaseSaveHeader_RequestError(t *testing.T) {
	output := &bytes.Buffer{}
	_ = logger.SetLogLevel("core/indexer:TRACE")
	_ = logger.AddLogObserver(output, &logger.PlainFormatter{})

	localErr := errors.New("localErr")
	header := &dataBlock.Header{Nonce: 1}
	signerIndexes := []uint64{0, 1}
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			return localErr
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveHeader(header, signerIndexes, &dataBlock.Body{}, nil, 1)

	defer func() {
		_ = logger.RemoveLogObserver(output)
		_ = logger.SetLogLevel("core/indexer:INFO")
	}()

	require.True(t, strings.Contains(output.String(), localErr.Error()))
}

func TestElasticseachDatabaseSaveHeader_CheckRequestBody(t *testing.T) {
	header := &dataBlock.Header{Nonce: 1}
	signerIndexes := []uint64{0, 1}

	miniBlock := &dataBlock.MiniBlock{Type: dataBlock.TxBlock}
	blockBody := &dataBlock.Body{
		MiniBlocks: []*dataBlock.MiniBlock{
			miniBlock,
		},
	}

	arguments := createMockElasticsearchDatabaseArgs()

	mbHash, _ := core.CalculateHash(arguments.marshalizer, arguments.hasher, miniBlock)
	hexEncodedHash := hex.EncodeToString(mbHash)

	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			require.Equal(t, blockIndex, req.Index)

			var block Block
			blockBytes, _ := ioutil.ReadAll(req.Body)
			_ = json.Unmarshal(blockBytes, &block)
			require.Equal(t, header.Nonce, block.Nonce)
			require.Equal(t, hexEncodedHash, block.MiniBlocksHashes[0])
			require.Equal(t, signerIndexes, block.Validators)

			return nil
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveHeader(header, signerIndexes, blockBody, nil, 1)
}

func TestElasticseachSaveTransactions(t *testing.T) {
	output := &bytes.Buffer{}
	_ = logger.SetLogLevel("core/indexer:TRACE")
	_ = logger.AddLogObserver(output, &logger.PlainFormatter{})

	localErr := errors.New("localErr")
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoBulkRequestCalled: func(buff *bytes.Buffer, index string) error {
			return localErr
		},
	}

	body := newTestBlockBody()
	header := &dataBlock.Header{Nonce: 1, TxCount: 2}
	txPool := newTestTxPool()

	defer func() {
		_ = logger.RemoveLogObserver(output)
		_ = logger.SetLogLevel("core/indexer:INFO")
	}()

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveTransactions(body, header, txPool, 0)
	require.True(t, strings.Contains(output.String(), "indexing bulk of transactions"))
}

func TestElasticsearch_saveShardValidatorsPubKeys_RequestError(t *testing.T) {
	output := &bytes.Buffer{}
	_ = logger.SetLogLevel("core/indexer:TRACE")
	_ = logger.AddLogObserver(output, &logger.PlainFormatter{})
	shardId := uint32(0)
	epoch := uint32(0)
	valPubKeys := [][]byte{[]byte("key1"), []byte("key2")}
	localErr := errors.New("localErr")
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			return localErr
		},
	}
	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveShardValidatorsPubKeys(shardId, epoch, valPubKeys)

	defer func() {
		_ = logger.RemoveLogObserver(output)
		_ = logger.SetLogLevel("core/indexer:INFO")
	}()

	require.True(t, strings.Contains(output.String(), localErr.Error()))
}

func TestElasticsearch_saveShardValidatorsPubKeys(t *testing.T) {
	shardId := uint32(0)
	epoch := uint32(0)
	valPubKeys := [][]byte{[]byte("key1"), []byte("key2")}
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			require.Equal(t, fmt.Sprintf("%d_%d", shardId, epoch), req.DocumentID)
			return nil
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveShardValidatorsPubKeys(shardId, epoch, valPubKeys)
}

func TestElasticsearch_saveShardStatistics_reqError(t *testing.T) {
	output := &bytes.Buffer{}
	_ = logger.SetLogLevel("core/indexer:TRACE")
	_ = logger.AddLogObserver(output, &logger.PlainFormatter{})

	tpsBenchmark := &mock.TpsBenchmarkMock{}
	metaBlock := &dataBlock.MetaBlock{
		TxCount: 2, Nonce: 1,
		ShardInfo: []dataBlock.ShardData{{HeaderHash: []byte("hash")}},
	}
	tpsBenchmark.UpdateWithShardStats(metaBlock)

	localError := errors.New("local err")
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoBulkRequestCalled: func(buff *bytes.Buffer, index string) error {
			return localError
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveShardStatistics(tpsBenchmark)

	defer func() {
		_ = logger.RemoveLogObserver(output)
		_ = logger.SetLogLevel("core/indexer:INFO")
	}()

	require.True(t, strings.Contains(output.String(), localError.Error()))
}

func TestElasticsearch_saveShardStatistics(t *testing.T) {
	tpsBenchmark := &mock.TpsBenchmarkMock{}
	metaBlock := &dataBlock.MetaBlock{
		TxCount: 2, Nonce: 1,
		ShardInfo: []dataBlock.ShardData{{HeaderHash: []byte("hash")}},
	}
	tpsBenchmark.UpdateWithShardStats(metaBlock)

	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoBulkRequestCalled: func(buff *bytes.Buffer, index string) error {
			require.Equal(t, tpsIndex, index)
			return nil
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveShardStatistics(tpsBenchmark)
}

func TestElasticsearch_saveRoundInfo(t *testing.T) {
	roundInfo := RoundInfo{
		Index: 1, ShardId: 0, BlockWasProposed: true,
	}
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			require.Equal(t, strconv.FormatUint(uint64(roundInfo.ShardId), 10)+"_"+strconv.FormatUint(roundInfo.Index, 10), req.DocumentID)
			return nil
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveRoundInfo(roundInfo)
}

func TestElasticsearch_saveRoundInfoRequestError(t *testing.T) {
	output := &bytes.Buffer{}
	_ = logger.SetLogLevel("core/indexer:TRACE")
	_ = logger.AddLogObserver(output, &logger.PlainFormatter{})

	roundInfo := RoundInfo{}
	localError := errors.New("local err")
	arguments := createMockElasticsearchDatabaseArgs()
	dbWriter := &mock.DatabaseWriterStub{
		DoRequestCalled: func(req *esapi.IndexRequest) error {
			return localError
		},
	}

	elasticDatabase := newTestElasticSearchDatabase(dbWriter, arguments)
	elasticDatabase.SaveRoundInfo(roundInfo)

	defer func() {
		_ = logger.RemoveLogObserver(output)
		_ = logger.SetLogLevel("core/indexer:INFO")
	}()

	require.True(t, strings.Contains(output.String(), localError.Error()))
}

func TestUpdateMiniBlock(t *testing.T) {
	t.Skip("test must run only if you have an elasticsearch server on address http://localhost:9200")

	args := elasticSearchDatabaseArgs{
		url:         "http://localhost:9200",
		userName:    "basic_auth_username",
		password:    "basic_auth_password",
		marshalizer: &mock.MarshalizerMock{},
		hasher:      &mock.HasherMock{},
	}

	esDatabase, _ := newElasticSearchDatabase(args)

	header1 := &dataBlock.Header{
		ShardID: 0,
	}
	body1 := &dataBlock.Body{
		MiniBlocks: []*dataBlock.MiniBlock{
			{SenderShardID: 1, ReceiverShardID: 0, TxHashes: [][]byte{[]byte("hash12")}},
			{SenderShardID: 0, ReceiverShardID: 1, TxHashes: [][]byte{[]byte("hash1")}},
		},
	}

	header2 := &dataBlock.Header{
		ShardID: 1,
	}

	// insert
	esDatabase.SaveMiniblocks(header1, body1)
	// update
	esDatabase.SaveMiniblocks(header2, body1)
}

func TestUpdateTransaction(t *testing.T) {
	t.Skip("test must run only if you have an elasticsearch server on address http://localhost:9200")

	args := elasticSearchDatabaseArgs{
		url:                    "http://localhost:9200",
		userName:               "basic_auth_username",
		password:               "basic_auth_password",
		marshalizer:            &mock.MarshalizerMock{},
		hasher:                 &mock.HasherMock{},
		addressPubkeyConverter: &mock.PubkeyConverterMock{},
	}

	esDatabase, _ := newElasticSearchDatabase(args)

	txHash1 := []byte("txHash1")
	tx1 := &transaction.Transaction{
		GasPrice: 10,
		GasLimit: 500,
	}
	txHash2 := []byte("txHash2")
	sndAddr := []byte("snd")
	tx2 := &transaction.Transaction{
		GasPrice: 10,
		GasLimit: 500,
		SndAddr:  sndAddr,
	}
	txHash3 := []byte("txHash3")
	tx3 := &transaction.Transaction{}

	recHash1 := []byte("recHash1")
	rec1 := &receipt.Receipt{
		Value:  big.NewInt(100),
		TxHash: txHash1,
	}

	scHash1 := []byte("scHash1")
	scResult1 := &smartContractResult.SmartContractResult{
		OriginalTxHash: txHash1,
	}

	scHash2 := []byte("scHash2")
	scResult2 := &smartContractResult.SmartContractResult{
		OriginalTxHash: txHash2,
		RcvAddr:        sndAddr,
		GasLimit:       500,
		GasPrice:       1,
		Value:          big.NewInt(150),
	}

	rTx1Hash := []byte("rTxHash1")
	rTx1 := &rewardTx.RewardTx{
		Round: 1113,
	}
	rTx2Hash := []byte("rTxHash2")
	rTx2 := &rewardTx.RewardTx{
		Round: 1114,
	}

	body := &dataBlock.Body{
		MiniBlocks: []*dataBlock.MiniBlock{
			{
				TxHashes: [][]byte{txHash1, txHash2},
				Type:     dataBlock.TxBlock,
			},
			{
				TxHashes: [][]byte{txHash3},
				Type:     dataBlock.TxBlock,
			},
			{
				Type:     dataBlock.RewardsBlock,
				TxHashes: [][]byte{rTx1Hash, rTx2Hash},
			},
			{
				TxHashes: [][]byte{recHash1},
				Type:     dataBlock.ReceiptBlock,
			},
			{
				TxHashes: [][]byte{scHash1, scHash2},
				Type:     dataBlock.SmartContractResultBlock,
			},
		},
	}
	header := &dataBlock.Header{}
	txPool := map[string]data.TransactionHandler{
		string(txHash1):  tx1,
		string(txHash2):  tx2,
		string(txHash3):  tx3,
		string(recHash1): rec1,
		string(rTx1Hash): rTx1,
		string(rTx2Hash): rTx2,
	}

	body.MiniBlocks[0].ReceiverShardID = 1
	// insert
	esDatabase.SaveTransactions(body, header, txPool, 0)

	header.TimeStamp = 1234
	txPool = map[string]data.TransactionHandler{
		string(txHash1): tx1,
		string(txHash2): tx2,
		string(scHash1): scResult1,
		string(scHash2): scResult2,
	}

	// update
	esDatabase.SaveTransactions(body, header, txPool, 1)
}

func TestTrimSliceInBulks(t *testing.T) {
	t.Parallel()

	sliceSize := 9500
	bulkSize := 1000

	testSlice := make([]int, sliceSize)
	bulks := make([][]int, sliceSize/bulkSize+1)
	bulksBigCapacity1 := make([][]int, sliceSize/bulkSize+1)
	bulksBigCapacity2 := make([][]int, sliceSize/bulkSize+1)

	for i := 0; i < sliceSize; i++ {
		testSlice[i] = i
	}

	for i := 0; i < len(bulks); i++ {
		if i == len(bulks)-1 {
			bulks[i] = append([]int(nil), testSlice[i*bulkSize:]...)
			bulksBigCapacity1[i] = append(bulksBigCapacity1[i], testSlice[i*bulkSize:]...)
			bulksBigCapacity2[i] = testSlice[i*bulkSize:]
			continue
		}

		bulks[i] = append([]int(nil), testSlice[i*bulkSize:(i+1)*bulkSize]...)
		bulksBigCapacity1[i] = append(bulksBigCapacity1[i], testSlice[i*bulkSize:(i+1)*bulkSize]...)
		bulksBigCapacity2[i] = testSlice[i*bulkSize : (i+1)*bulkSize]
	}

	require.Equal(t, len(bulks), sliceSize/bulkSize+1)
	require.Equal(t, len(bulksBigCapacity1), sliceSize/bulkSize+1)
	require.Equal(t, len(bulksBigCapacity2), sliceSize/bulkSize+1)
}
