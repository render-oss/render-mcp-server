package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/render-oss/render-mcp-server/pkg/client"
	metricstypes "github.com/render-oss/render-mcp-server/pkg/client/metrics"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// Test constants
var (
	testTimestamp = time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
)

// MockClientWithResponses implements MetricsClient interface for testing
type MockClientWithResponses struct {
	mock.Mock
}

func (m *MockClientWithResponses) GetCpuWithResponse(ctx context.Context, params *client.GetCpuParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetCpuResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetMemoryWithResponse(ctx context.Context, params *client.GetMemoryParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetMemoryResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetHttpRequestsWithResponse(ctx context.Context, params *client.GetHttpRequestsParams, reqEditors ...client.RequestEditorFn) (*client.GetHttpRequestsResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetHttpRequestsResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetHttpLatencyWithResponse(ctx context.Context, params *client.GetHttpLatencyParams, reqEditors ...client.RequestEditorFn) (*client.GetHttpLatencyResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetHttpLatencyResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetActiveConnectionsWithResponse(ctx context.Context, params *client.GetActiveConnectionsParams, reqEditors ...client.RequestEditorFn) (*client.GetActiveConnectionsResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetActiveConnectionsResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetInstanceCountWithResponse(ctx context.Context, params *client.GetInstanceCountParams, reqEditors ...client.RequestEditorFn) (*client.GetInstanceCountResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetInstanceCountResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetCpuLimitWithResponse(ctx context.Context, params *client.GetCpuLimitParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuLimitResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetCpuLimitResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetCpuTargetWithResponse(ctx context.Context, params *client.GetCpuTargetParams, reqEditors ...client.RequestEditorFn) (*client.GetCpuTargetResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetCpuTargetResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetMemoryLimitWithResponse(ctx context.Context, params *client.GetMemoryLimitParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryLimitResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetMemoryLimitResponse), args.Error(1)
}

func (m *MockClientWithResponses) GetMemoryTargetWithResponse(ctx context.Context, params *client.GetMemoryTargetParams, reqEditors ...client.RequestEditorFn) (*client.GetMemoryTargetResponse, error) {
	args := m.Called(ctx, params, reqEditors)
	return args.Get(0).(*client.GetMemoryTargetResponse), args.Error(1)
}

// MetricsTestSuite provides shared setup and utilities for metrics tests
type MetricsTestSuite struct {
	suite.Suite
	mockClient *MockClientWithResponses
	repo       *Repo
	ctx        context.Context
}

func (s *MetricsTestSuite) SetupTest() {
	s.mockClient = &MockClientWithResponses{}
	s.repo = NewRepo(s.mockClient)
	s.ctx = session.ContextWithStdioSession(context.Background())
	
	// Set up a workspace for tests
	sess := session.FromContext(s.ctx)
	err := sess.SetWorkspace(s.ctx, "test-workspace-123")
	if err != nil {
		s.T().Fatalf("Failed to set up test workspace: %v", err)
	}
}

func (s *MetricsTestSuite) TearDownTest() {
	s.mockClient.AssertExpectations(s.T())
}

// Helper methods for the test suite
func (s *MetricsTestSuite) createBasicRequest(resourceID string, metricTypes ...MetricType) MetricsRequest {
	return MetricsRequest{
		ResourceID:  resourceID,
		MetricTypes: metricTypes,
	}
}

func (s *MetricsTestSuite) assertBasicResponse(resp *MetricsResponse, expectedResourceID string, expectedMetricCount int) {
	s.Require().NotNil(resp)
	s.Equal(expectedResourceID, resp.ResourceID)
	s.Len(resp.Metrics, expectedMetricCount)

	for _, metric := range resp.Metrics {
		s.NotEmpty(metric.Data)
	}
}

// Factory methods for test data creation

func NewMockCPUResponse(value float32) *client.GetCpuResponse {
	return &client.GetCpuResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "cpu",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockMemoryResponse(value float32) *client.GetMemoryResponse {
	return &client.GetMemoryResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "bytes",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockHTTPRequestsResponse(value float32, statusCode string) *client.GetHttpRequestsResponse {
	return &client.GetHttpRequestsResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "unitless",
			Labels: []metricstypes.Label{{Field: "statusCode", Value: statusCode}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockHTTPLatencyResponse(value float32, path string) *client.GetHttpLatencyResponse {
	labels := []metricstypes.Label{{Field: "instance", Value: "srv-123"}}
	if path != "" {
		labels = append(labels, metricstypes.Label{Field: "path", Value: path})
	}

	return &client.GetHttpLatencyResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "ms",
			Labels: labels,
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockActiveConnectionsResponse(value float32) *client.GetActiveConnectionsResponse {
	return &client.GetActiveConnectionsResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "connections",
			Labels: []metricstypes.Label{{Field: "instance", Value: "kv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockInstanceCountResponse(value float32) *client.GetInstanceCountResponse {
	return &client.GetInstanceCountResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "instances",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockCPULimitResponse(value float32) *client.GetCpuLimitResponse {
	return &client.GetCpuLimitResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "cpu",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockCPUTargetResponse(value float32) *client.GetCpuTargetResponse {
	return &client.GetCpuTargetResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "cpu",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockMemoryLimitResponse(value float32) *client.GetMemoryLimitResponse {
	return &client.GetMemoryLimitResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "bytes",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockMemoryTargetResponse(value float32) *client.GetMemoryTargetResponse {
	return &client.GetMemoryTargetResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		JSON200: &metricstypes.TimeSeriesCollection{{
			Unit:   "bytes",
			Labels: []metricstypes.Label{{Field: "instance", Value: "srv-123-abc"}},
			Values: []metricstypes.TimeSeriesValue{{Timestamp: testTimestamp, Value: value}},
		}},
	}
}

func NewMockErrorResponse(statusCode int) *http.Response {
	return &http.Response{StatusCode: statusCode}
}

// Parameter matchers for better test debugging

func CPUParamsMatcher(expectedResource string, expectedResolution *float32, expectedAggregation *metricstypes.ApplicationMetricAggregationMethod) interface{} {
	return mock.MatchedBy(func(params *client.GetCpuParams) bool {
		if params.Resource == nil || string(*params.Resource) != expectedResource {
			return false
		}

		if expectedResolution != nil {
			if params.ResolutionSeconds == nil || float32(*params.ResolutionSeconds) != *expectedResolution {
				return false
			}
		}

		if expectedAggregation != nil {
			if params.AggregationMethod == nil || *params.AggregationMethod != *expectedAggregation {
				return false
			}
		}

		return true
	})
}

func HTTPRequestsParamsMatcher(expectedResource string, expectedAggregation *metricstypes.HttpAggregateBy, expectedResolution *float32, expectedHost *metricstypes.HostQueryParam, expectedPath *metricstypes.PathQueryParam) interface{} {
	return mock.MatchedBy(func(params *client.GetHttpRequestsParams) bool {
		if params.Resource == nil || string(*params.Resource) != expectedResource {
			return false
		}

		if expectedAggregation != nil {
			if params.AggregateBy == nil || *params.AggregateBy != *expectedAggregation {
				return false
			}
		}

		if expectedResolution != nil {
			if params.ResolutionSeconds == nil || float32(*params.ResolutionSeconds) != *expectedResolution {
				return false
			}
		}

		if expectedHost != nil {
			if params.Host == nil || string(*params.Host) != string(*expectedHost) {
				return false
			}
		} else if params.Host != nil {
			return false // Expected nil host but got one
		}

		if expectedPath != nil {
			if params.Path == nil || string(*params.Path) != string(*expectedPath) {
				return false
			}
		} else if params.Path != nil {
			return false // Expected nil path but got one
		}

		return true
	})
}

func HTTPLatencyParamsMatcher(expectedResource string, expectedQuantile *metricstypes.Quantile, expectedHost *metricstypes.HostQueryParam, expectedPath *metricstypes.PathQueryParam) interface{} {
	return mock.MatchedBy(func(params *client.GetHttpLatencyParams) bool {
		if params.Resource == nil || string(*params.Resource) != expectedResource {
			return false
		}

		if expectedQuantile != nil {
			if params.Quantile == nil || float32(*params.Quantile) != float32(*expectedQuantile) {
				return false
			}
		}

		if expectedHost != nil {
			if params.Host == nil || string(*params.Host) != string(*expectedHost) {
				return false
			}
		} else if params.Host != nil {
			return false // Expected nil host but got one
		}

		if expectedPath != nil {
			if params.Path == nil || string(*params.Path) != string(*expectedPath) {
				return false
			}
		} else if params.Path != nil {
			return false // Expected nil path but got one
		}

		return true
	})
}
