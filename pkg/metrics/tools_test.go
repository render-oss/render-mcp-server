package metrics

import (
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/client"
	metricstypes "github.com/render-oss/render-mcp-server/pkg/client/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// The tests in this file are all written by AI, and are meant for the benefit of AI tooling.
// They're helpful to validate that there's been no major regressions, but won't necessarily be
// the most helpful tests.

// Integration Tests using Test Suite

func TestMetricsIntegration(t *testing.T) {
	suite.Run(t, new(MetricsIntegrationSuite))
}

type MetricsIntegrationSuite struct {
	MetricsTestSuite
}

func (s *MetricsIntegrationSuite) TestSuccessfulMetricsFetching() {
	tests := []struct {
		name       string
		metricType MetricType
		setupMock  func()
		value      float32
	}{
		{
			name:       "CPU metrics success",
			metricType: MetricTypeCPUUsage,
			setupMock: func() {
				s.mockClient.On("GetCpuWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockCPUResponse(0.75), nil)
			},
			value: 0.75,
		},
		{
			name:       "Memory metrics success",
			metricType: MetricTypeMemoryUsage,
			setupMock: func() {
				s.mockClient.On("GetMemoryWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockMemoryResponse(1024000000), nil)
			},
			value: 1024000000,
		},
		{
			name:       "HTTP requests success",
			metricType: MetricTypeHTTPRequestCount,
			setupMock: func() {
				s.mockClient.On("GetHttpRequestsWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockHTTPRequestsResponse(150, "200"), nil)
			},
			value: 150,
		},
		{
			name:       "Active connections success",
			metricType: MetricTypeActiveConnections,
			setupMock: func() {
				s.mockClient.On("GetActiveConnectionsWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockActiveConnectionsResponse(25), nil)
			},
			value: 25,
		},
		{
			name:       "Instance count success",
			metricType: MetricTypeInstanceCount,
			setupMock: func() {
				s.mockClient.On("GetInstanceCountWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockInstanceCountResponse(3), nil)
			},
			value: 3,
		},
		{
			name:       "HTTP latency success",
			metricType: MetricTypeHTTPLatency,
			setupMock: func() {
				s.mockClient.On("GetHttpLatencyWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockHTTPLatencyResponse(125.5, ""), nil)
			},
			value: 125.5,
		},
		{
			name:       "CPU limit success",
			metricType: MetricTypeCPULimit,
			setupMock: func() {
				s.mockClient.On("GetCpuLimitWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockCPULimitResponse(1.0), nil)
			},
			value: 1.0,
		},
		{
			name:       "CPU target success",
			metricType: MetricTypeCPUTarget,
			setupMock: func() {
				s.mockClient.On("GetCpuTargetWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockCPUTargetResponse(0.8), nil)
			},
			value: 0.8,
		},
		{
			name:       "Memory limit success",
			metricType: MetricTypeMemoryLimit,
			setupMock: func() {
				s.mockClient.On("GetMemoryLimitWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockMemoryLimitResponse(2147483648), nil)
			},
			value: 2147483648,
		},
		{
			name:       "Memory target success",
			metricType: MetricTypeMemoryTarget,
			setupMock: func() {
				s.mockClient.On("GetMemoryTargetWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(NewMockMemoryTargetResponse(1073741824), nil)
			},
			value: 1073741824,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest() // Fresh setup for each subtest
			tt.setupMock()

			req := s.createBasicRequest("srv-123", tt.metricType)
			resp, err := s.repo.GetMetrics(s.ctx, req)

			s.NoError(err)
			s.assertBasicResponse(resp, "srv-123", 1)
			s.Equal(tt.metricType, resp.Metrics[0].Type)
			s.Equal(tt.value, resp.Metrics[0].Data[0].Values[0].Value)
		})
	}
}

func (s *MetricsIntegrationSuite) TestMultipleMetrics() {
	s.mockClient.On("GetCpuWithResponse", mock.Anything, mock.Anything, mock.Anything).
		Return(NewMockCPUResponse(0.65), nil)
	s.mockClient.On("GetMemoryWithResponse", mock.Anything, mock.Anything, mock.Anything).
		Return(NewMockMemoryResponse(2048000000), nil)

	req := s.createBasicRequest("srv-123", MetricTypeCPUUsage, MetricTypeMemoryUsage)
	resp, err := s.repo.GetMetrics(s.ctx, req)

	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 2)

	// Verify both metrics are present
	metricTypes := make(map[MetricType]bool)
	for _, metric := range resp.Metrics {
		metricTypes[metric.Type] = true
	}
	s.True(metricTypes[MetricTypeCPUUsage])
	s.True(metricTypes[MetricTypeMemoryUsage])
}

// Error Scenarios Test Suite

func TestMetricsErrors(t *testing.T) {
	suite.Run(t, new(MetricsErrorSuite))
}

type MetricsErrorSuite struct {
	MetricsTestSuite
}

func (s *MetricsErrorSuite) TestAPIErrors() {
	tests := []struct {
		name        string
		metricType  MetricType
		setupMock   func()
		expectedErr string
		shouldError bool
	}{
		{
			name:       "API returns 500 error",
			metricType: MetricTypeCPUUsage,
			setupMock: func() {
				resp := &client.GetCpuResponse{HTTPResponse: NewMockErrorResponse(500)}
				s.mockClient.On("GetCpuWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(resp, nil)
			},
			expectedErr: "API returned status 500",
			shouldError: true,
		},
		{
			name:       "Network error",
			metricType: MetricTypeMemoryUsage,
			setupMock: func() {
				s.mockClient.On("GetMemoryWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return((*client.GetMemoryResponse)(nil), assert.AnError)
			},
			expectedErr: "failed to get memory metrics",
			shouldError: true,
		},
		{
			name:       "HTTP latency 400 error (hobby tier limitation)",
			metricType: MetricTypeHTTPLatency,
			setupMock: func() {
				resp := &client.GetHttpLatencyResponse{HTTPResponse: NewMockErrorResponse(400)}
				s.mockClient.On("GetHttpLatencyWithResponse", mock.Anything, mock.Anything, mock.Anything).
					Return(resp, nil)
			},
			expectedErr: "",
			shouldError: false, // 400 errors for HTTP latency should return empty data, not error
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			tt.setupMock()

			req := s.createBasicRequest("srv-123", tt.metricType)
			resp, err := s.repo.GetMetrics(s.ctx, req)

			if tt.shouldError {
				s.Error(err)
				s.Contains(err.Error(), tt.expectedErr)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Len(resp.Metrics, 1)
				s.Empty(resp.Metrics[0].Data) // Should be empty for 400 errors
			}
		})
	}
}

func (s *MetricsErrorSuite) TestUnsupportedMetricType() {
	req := s.createBasicRequest("srv-123", "invalid_metric")
	resp, err := s.repo.GetMetrics(s.ctx, req)

	s.Error(err)
	s.Contains(err.Error(), "unsupported metric type: invalid_metric")
	s.Nil(resp)
}

// Parameter Propagation Test Suite

func TestParameterPropagation(t *testing.T) {
	suite.Run(t, new(ParameterPropagationSuite))
}

type ParameterPropagationSuite struct {
	MetricsTestSuite
}

func (s *ParameterPropagationSuite) TestCPUParameters() {
	s.mockClient.On("GetCpuWithResponse", mock.Anything,
		CPUParamsMatcher("srv-123", ptr(float32(120)), ptr(metricstypes.MAX)),
		mock.Anything).
		Return(NewMockCPUResponse(0.75), nil)

	req := MetricsRequest{
		ResourceID:                "srv-123",
		MetricTypes:               []MetricType{MetricTypeCPUUsage},
		Resolution:                ptr(float32(120)),
		CpuUsageAggregationMethod: ptr(metricstypes.MAX),
	}

	resp, err := s.repo.GetMetrics(s.ctx, req)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

func (s *ParameterPropagationSuite) TestHTTPRequestsParameters() {
	s.mockClient.On("GetHttpRequestsWithResponse", mock.Anything,
		HTTPRequestsParamsMatcher("srv-123", ptr(metricstypes.HttpAggregateByStatusCode), ptr(float32(60)), nil, nil),
		mock.Anything).
		Return(NewMockHTTPRequestsResponse(150, "200"), nil)

	req := MetricsRequest{
		ResourceID:                  "srv-123",
		MetricTypes:                 []MetricType{MetricTypeHTTPRequestCount},
		Resolution:                  ptr(float32(60)),
		AggregateHttpRequestCountBy: ptr(metricstypes.HttpAggregateByStatusCode),
	}

	resp, err := s.repo.GetMetrics(s.ctx, req)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

func (s *ParameterPropagationSuite) TestHTTPLatencyParameters() {
	s.mockClient.On("GetHttpLatencyWithResponse", mock.Anything,
		HTTPLatencyParamsMatcher("srv-123", ptr(metricstypes.Quantile(0.99)), nil, ptr(metricstypes.PathQueryParam("/api/users"))),
		mock.Anything).
		Return(NewMockHTTPLatencyResponse(125.5, "/api/users"), nil)

	req := MetricsRequest{
		ResourceID:          "srv-123",
		MetricTypes:         []MetricType{MetricTypeHTTPLatency},
		HttpLatencyQuantile: ptr(metricstypes.Quantile(0.99)),
		HttpPath:            ptr(metricstypes.PathQueryParam("/api/users")),
	}

	resp, err := s.repo.GetMetrics(s.ctx, req)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

func (s *ParameterPropagationSuite) TestHTTPLatencyDefaults() {
	s.mockClient.On("GetHttpLatencyWithResponse", mock.Anything,
		HTTPLatencyParamsMatcher("srv-123", ptr(metricstypes.Quantile(0.95)), nil, nil),
		mock.Anything).
		Return(NewMockHTTPLatencyResponse(85.2, ""), nil)

	req := s.createBasicRequest("srv-123", MetricTypeHTTPLatency)
	resp, err := s.repo.GetMetrics(s.ctx, req)

	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

func (s *ParameterPropagationSuite) TestHTTPFilteringParameters() {
	// Test HTTP requests with host and path filtering
	s.mockClient.On("GetHttpRequestsWithResponse", mock.Anything,
		HTTPRequestsParamsMatcher("srv-123", nil, nil, ptr(metricstypes.HostQueryParam("api.example.com")), ptr(metricstypes.PathQueryParam("/api/users"))),
		mock.Anything).
		Return(NewMockHTTPRequestsResponse(75, "200"), nil)

	reqHTTPRequests := MetricsRequest{
		ResourceID:  "srv-123",
		MetricTypes: []MetricType{MetricTypeHTTPRequestCount},
		HttpHost:    ptr(metricstypes.HostQueryParam("api.example.com")),
		HttpPath:    ptr(metricstypes.PathQueryParam("/api/users")),
	}

	resp, err := s.repo.GetMetrics(s.ctx, reqHTTPRequests)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)

	// Test HTTP latency with host filtering
	s.SetupTest() // Fresh mock for next test
	s.mockClient.On("GetHttpLatencyWithResponse", mock.Anything,
		HTTPLatencyParamsMatcher("srv-123", ptr(metricstypes.Quantile(0.95)), ptr(metricstypes.HostQueryParam("api.example.com")), nil),
		mock.Anything).
		Return(NewMockHTTPLatencyResponse(150.0, ""), nil)

	reqHTTPLatency := MetricsRequest{
		ResourceID:  "srv-123",
		MetricTypes: []MetricType{MetricTypeHTTPLatency},
		HttpHost:    ptr(metricstypes.HostQueryParam("api.example.com")),
	}

	resp, err = s.repo.GetMetrics(s.ctx, reqHTTPLatency)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

// Boundary Conditions Test Suite

func TestBoundaryConditions(t *testing.T) {
	suite.Run(t, new(BoundaryConditionsSuite))
}

type BoundaryConditionsSuite struct {
	MetricsTestSuite
}

func (s *BoundaryConditionsSuite) TestEmptyMetricTypes() {
	req := MetricsRequest{
		ResourceID:  "srv-123",
		MetricTypes: []MetricType{}, // Empty metric types
	}

	resp, err := s.repo.GetMetrics(s.ctx, req)
	s.NoError(err) // Should not error, just return empty metrics
	s.NotNil(resp)
	s.Equal("srv-123", resp.ResourceID)
	s.Empty(resp.Metrics)
}

func (s *BoundaryConditionsSuite) TestNilParameters() {
	req := MetricsRequest{
		ResourceID:                  "srv-123",
		MetricTypes:                 []MetricType{MetricTypeCPUUsage},
		StartTime:                   nil,
		EndTime:                     nil,
		Resolution:                  nil,
		CpuUsageAggregationMethod:   nil,
		AggregateHttpRequestCountBy: nil,
		HttpLatencyQuantile:         nil,
		HttpHost:                    nil,
		HttpPath:                    nil,
	}

	s.mockClient.On("GetCpuWithResponse", mock.Anything, mock.Anything, mock.Anything).
		Return(NewMockCPUResponse(0.5), nil)

	resp, err := s.repo.GetMetrics(s.ctx, req)
	s.NoError(err)
	s.assertBasicResponse(resp, "srv-123", 1)
}

// Essential structure validation tests

func TestMetricDataStructure(t *testing.T) {
	// Test that MetricData no longer has a redundant Unit field
	data := MetricData{
		Type: MetricTypeCPUUsage,
		Data: metricstypes.TimeSeriesCollection{
			{
				Unit: "cpu",
				Labels: []metricstypes.Label{
					{Field: "instance", Value: "srv-123-abc"},
				},
				Values: []metricstypes.TimeSeriesValue{
					{Timestamp: testTimestamp, Value: 0.5},
				},
			},
		},
	}

	// Verify the structure contains the metric type and data
	if data.Type != MetricTypeCPUUsage {
		t.Errorf("expected metric type %s, got %s", MetricTypeCPUUsage, data.Type)
	}

	if len(data.Data) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(data.Data))
	}

	// Verify units are in individual TimeSeries objects, not at the top level
	if data.Data[0].Unit != "cpu" {
		t.Errorf("expected unit 'cpu' in TimeSeries, got '%s'", data.Data[0].Unit)
	}
}

// Helper function to create pointer values for tests
func ptr[T any](v T) *T {
	return &v
}
