package node_test

import (
	"bytes"
	"encoding/json"
	errs "errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ElrondNetwork/elrond-go/api/errors"
	"github.com/ElrondNetwork/elrond-go/api/mock"
	"github.com/ElrondNetwork/elrond-go/api/node"
	"github.com/ElrondNetwork/elrond-go/api/wrapper"
	"github.com/ElrondNetwork/elrond-go/config"
	"github.com/ElrondNetwork/elrond-go/core/statistics"
	"github.com/ElrondNetwork/elrond-go/debug"
	"github.com/ElrondNetwork/elrond-go/heartbeat/data"
	"github.com/ElrondNetwork/elrond-go/node/external"
	"github.com/ElrondNetwork/elrond-go/statusHandler"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type GeneralResponse struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

type StatusResponse struct {
	GeneralResponse
	Running bool `json:"running"`
}

type QueryResponse struct {
	GeneralResponse
	Result []string `json:"result"`
}

type StatisticsResponse struct {
	GeneralResponse
	Statistics struct {
		LiveTPS               float32 `json:"liveTPS"`
		PeakTPS               float32 `json:"peakTPS"`
		NrOfShards            uint32  `json:"nrOfShards"`
		BlockNumber           uint64  `json:"blockNumber"`
		RoundTime             uint32  `json:"roundTime"`
		AverageBlockTxCount   float32 `json:"averageBlockTxCount"`
		LastBlockTxCount      uint32  `json:"lastBlockTxCount"`
		TotalProcessedTxCount uint32  `json:"totalProcessedTxCount"`
	} `json:"statistics"`
}

func init() {
	gin.SetMode(gin.TestMode)
}

func TestStartNode_FailsWithoutFacade(t *testing.T) {
	t.Parallel()
	ws := startNodeServer(nil)
	defer func() {
		r := recover()
		assert.Nil(t, r, "Not providing elrondFacade context should panic")
	}()
	req, _ := http.NewRequest("GET", "/node/start", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)
}

//------- Heartbeatstatus

func TestHeartbeatStatus_FailsWithoutFacade(t *testing.T) {
	t.Parallel()

	ws := startNodeServer(nil)
	defer func() {
		r := recover()

		assert.NotNil(t, r, "Not providing elrondFacade context should panic")
	}()
	req, _ := http.NewRequest("GET", "/node/heartbeatstatus", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)
}

func TestHeartbeatstatus_FailsWithWrongFacadeTypeConversion(t *testing.T) {
	t.Parallel()

	ws := startNodeServerWrongFacade()
	req, _ := http.NewRequest("GET", "/node/heartbeatstatus", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	statusRsp := StatusResponse{}
	loadResponse(resp.Body, &statusRsp)

	assert.Equal(t, resp.Code, http.StatusInternalServerError)
	assert.Equal(t, statusRsp.Error, errors.ErrInvalidAppContext.Error())
}

func TestHeartbeatstatus_FromFacadeErrors(t *testing.T) {
	t.Parallel()

	errExpected := errs.New("expected error")
	facade := mock.Facade{
		GetHeartbeatsHandler: func() ([]data.PubKeyHeartbeat, error) {
			return nil, errExpected
		},
	}
	ws := startNodeServer(&facade)
	req, _ := http.NewRequest("GET", "/node/heartbeatstatus", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	statusRsp := StatusResponse{}
	loadResponse(resp.Body, &statusRsp)

	assert.Equal(t, resp.Code, http.StatusInternalServerError)
	assert.Equal(t, errExpected.Error(), statusRsp.Error)
}

func TestHeartbeatstatus(t *testing.T) {
	t.Parallel()

	hbStatus := []data.PubKeyHeartbeat{
		{
			PublicKey:       "pk1",
			TimeStamp:       time.Now(),
			MaxInactiveTime: data.Duration{Duration: 0},
			IsActive:        true,
			ReceivedShardID: uint32(0),
		},
	}
	facade := mock.Facade{
		GetHeartbeatsHandler: func() (heartbeats []data.PubKeyHeartbeat, e error) {
			return hbStatus, nil
		},
	}
	ws := startNodeServer(&facade)
	req, _ := http.NewRequest("GET", "/node/heartbeatstatus", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	statusRsp := StatusResponse{}
	loadResponseAsString(resp.Body, &statusRsp)

	assert.Equal(t, resp.Code, http.StatusOK)
	assert.NotEqual(t, "", statusRsp.Message)
}

func TestStatistics_FailsWithoutFacade(t *testing.T) {
	t.Parallel()
	ws := startNodeServer(nil)
	defer func() {
		r := recover()
		assert.NotNil(t, r, "Not providing elrondFacade context should panic")
	}()
	req, _ := http.NewRequest("GET", "/node/statistics", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)
}

func TestStatistics_FailsWithWrongFacadeTypeConversion(t *testing.T) {
	t.Parallel()
	ws := startNodeServerWrongFacade()
	req, _ := http.NewRequest("GET", "/node/statistics", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	statisticsRsp := StatisticsResponse{}
	loadResponse(resp.Body, &statisticsRsp)
	assert.Equal(t, resp.Code, http.StatusInternalServerError)
	assert.Equal(t, statisticsRsp.Error, errors.ErrInvalidAppContext.Error())
}

func TestStatistics_ReturnsSuccessfully(t *testing.T) {
	nrOfShards := uint32(10)
	roundTime := uint64(4)
	benchmark, _ := statistics.NewTPSBenchmark(nrOfShards, roundTime)

	facade := mock.Facade{}
	facade.TpsBenchmarkHandler = func() *statistics.TpsBenchmark {
		return benchmark
	}

	ws := startNodeServer(&facade)
	req, _ := http.NewRequest("GET", "/node/statistics", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	statisticsRsp := StatisticsResponse{}
	loadResponse(resp.Body, &statisticsRsp)
	assert.Equal(t, resp.Code, http.StatusOK)
	assert.Equal(t, statisticsRsp.Statistics.NrOfShards, nrOfShards)
}

func TestStatusMetrics_ShouldDisplayNonP2pMetrics(t *testing.T) {
	statusMetricsProvider := statusHandler.NewStatusMetrics()
	key := "test-details-key"
	value := "test-details-value"
	statusMetricsProvider.SetStringValue(key, value)

	p2pKey := "a_p2p_specific_key"
	statusMetricsProvider.SetStringValue(p2pKey, "p2p value")

	facade := mock.Facade{}
	facade.StatusMetricsHandler = func() external.StatusMetricsHandler {
		return statusMetricsProvider
	}

	ws := startNodeServer(&facade)
	req, _ := http.NewRequest("GET", "/node/status", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	respBytes, _ := ioutil.ReadAll(resp.Body)
	respStr := string(respBytes)
	assert.Equal(t, resp.Code, http.StatusOK)

	keyAndValueFoundInResponse := strings.Contains(respStr, key) && strings.Contains(respStr, value)
	assert.True(t, keyAndValueFoundInResponse)
	assert.False(t, strings.Contains(respStr, p2pKey))
}

func TestP2PStatusMetrics_ShouldDisplayNonP2pMetrics(t *testing.T) {
	statusMetricsProvider := statusHandler.NewStatusMetrics()
	key := "test-details-key"
	value := "test-details-value"
	statusMetricsProvider.SetStringValue(key, value)

	p2pKey := "a_p2p_specific_key"
	p2pValue := "p2p value"
	statusMetricsProvider.SetStringValue(p2pKey, p2pValue)

	facade := mock.Facade{}
	facade.StatusMetricsHandler = func() external.StatusMetricsHandler {
		return statusMetricsProvider
	}

	ws := startNodeServer(&facade)
	req, _ := http.NewRequest("GET", "/node/p2pstatus", nil)
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	respBytes, _ := ioutil.ReadAll(resp.Body)
	respStr := string(respBytes)
	assert.Equal(t, resp.Code, http.StatusOK)

	keyAndValueFoundInResponse := strings.Contains(respStr, p2pKey) && strings.Contains(respStr, p2pValue)
	assert.True(t, keyAndValueFoundInResponse)

	assert.False(t, strings.Contains(respStr, key))
}

func TestQueryDebug_GetQueryErrorsShouldErr(t *testing.T) {
	t.Parallel()

	expectedErr := errs.New("expected error")
	facade := &mock.Facade{
		GetQueryHandlerCalled: func(name string) (handler debug.QueryHandler, err error) {
			return nil, expectedErr
		},
	}

	qdr := &node.QueryDebugRequest{}
	jsonStr, _ := json.Marshal(qdr)

	ws := startNodeServerWithFacade(facade)
	req, _ := http.NewRequest("POST", "/node/debug", bytes.NewBuffer(jsonStr))
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	queryResponse := &GeneralResponse{}
	loadResponse(resp.Body, queryResponse)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, queryResponse.Error, expectedErr.Error())
}

func TestQueryDebug_GetQueryShouldWork(t *testing.T) {
	t.Parallel()

	str1 := "aaa"
	str2 := "bbb"
	facade := &mock.Facade{
		GetQueryHandlerCalled: func(name string) (handler debug.QueryHandler, err error) {
			return &mock.QueryHandlerStub{
					QueryCalled: func(search string) []string {
						return []string{str1, str2}
					},
				},
				nil
		},
	}

	qdr := &node.QueryDebugRequest{}
	jsonStr, _ := json.Marshal(qdr)

	ws := startNodeServerWithFacade(facade)
	req, _ := http.NewRequest("POST", "/node/debug", bytes.NewBuffer(jsonStr))
	resp := httptest.NewRecorder()
	ws.ServeHTTP(resp, req)

	queryResponse := &QueryResponse{}
	loadResponse(resp.Body, queryResponse)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, queryResponse.Result, str1)
	assert.Contains(t, queryResponse.Result, str2)
}

func loadResponse(rsp io.Reader, destination interface{}) {
	jsonParser := json.NewDecoder(rsp)
	err := jsonParser.Decode(destination)
	if err != nil {
		logError(err)
	}
}

func loadResponseAsString(rsp io.Reader, response *StatusResponse) {
	buff, err := ioutil.ReadAll(rsp)
	if err != nil {
		logError(err)
		return
	}

	response.Message = string(buff)
}

func logError(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func startNodeServer(handler node.FacadeHandler) *gin.Engine {
	server := startNodeServerWithFacade(handler)
	return server
}

func startNodeServerWrongFacade() *gin.Engine {
	return startNodeServerWithFacade(mock.WrongFacade{})
}

func startNodeServerWithFacade(facade interface{}) *gin.Engine {
	ws := gin.New()
	ws.Use(cors.Default())
	if facade != nil {
		ws.Use(func(c *gin.Context) {
			c.Set("elrondFacade", facade)
		})
	}

	ginNodeRoutes := ws.Group("/node")
	nodeRoutes, _ := wrapper.NewRouterWrapper("node", ginNodeRoutes, getRoutesConfig())
	node.Routes(nodeRoutes)
	return ws
}

func getRoutesConfig() config.ApiRoutesConfig {
	return config.ApiRoutesConfig{
		APIPackages: map[string]config.APIPackageConfig{
			"node": {
				[]config.RouteConfig{
					{Name: "/status", Open: true},
					{Name: "/statistics", Open: true},
					{Name: "/heartbeatstatus", Open: true},
					{Name: "/p2pstatus", Open: true},
					{Name: "/debug", Open: true},
				},
			},
		},
	}
}
