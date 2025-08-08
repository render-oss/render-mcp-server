package metrics

import (
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/client"
)

func TestMetricTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		metric   MetricType
		expected string
	}{
		{"CPU metric", MetricTypeCPU, "cpu"},
		{"Memory metric", MetricTypeMemory, "memory"},
		{"HTTP metric", MetricTypeHTTP, "http"},
		{"Connections metric", MetricTypeConnections, "connections"},
		{"Instance count metric", MetricTypeInstanceCount, "instancecount"},
		{"HTTP errors metric", MetricTypeHTTPErrors, "httperrors"},
		{"Response time metric", MetricTypeResponseTime, "responsetime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.metric) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.metric))
			}
		})
	}
}

func TestNewRepo(t *testing.T) {
	mockClient := &client.ClientWithResponses{}
	repo := NewRepo(mockClient)

	if repo == nil {
		t.Fatal("expected repo to be created, got nil")
	}

	if repo.client != mockClient {
		t.Error("expected repo client to be set correctly")
	}
}

func TestMetricsRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		request MetricsRequest
		valid   bool
	}{
		{
			name: "Valid service request with CPU metrics",
			request: MetricsRequest{
				ResourceID:  "srv-123",
				MetricTypes: []MetricType{MetricTypeCPU},
			},
			valid: true,
		},
		{
			name: "Valid postgres request with memory metrics",
			request: MetricsRequest{
				ResourceID:  "pg-123",
				MetricTypes: []MetricType{MetricTypeMemory},
			},
			valid: true,
		},
		{
			name: "Valid datastore request with connections metrics",
			request: MetricsRequest{
				ResourceID:  "kv-123",
				MetricTypes: []MetricType{MetricTypeConnections},
			},
			valid: true,
		},
		{
			name: "Multiple metric types",
			request: MetricsRequest{
				ResourceID:  "srv-123",
				MetricTypes: []MetricType{MetricTypeCPU, MetricTypeMemory, MetricTypeHTTP},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation - ensure required fields are present
			if tt.request.ResourceID == "" {
				if tt.valid {
					t.Error("expected valid request to have resource ID")
				}
			}

			if len(tt.request.MetricTypes) == 0 {
				if tt.valid {
					t.Error("expected valid request to have metric types")
				}
			}
		})
	}
}

func TestMetricTypeValidation(t *testing.T) {
	validTypes := []string{"cpu", "memory", "http", "connections", "instancecount", "httperrors", "responsetime"}
	invalidTypes := []string{"disk", "network", "invalid", ""}

	for _, validType := range validTypes {
		metric := MetricType(validType)
		switch metric {
		case MetricTypeCPU, MetricTypeMemory, MetricTypeHTTP, MetricTypeConnections, MetricTypeInstanceCount, MetricTypeHTTPErrors, MetricTypeResponseTime:
			// Valid - test passes
		default:
			t.Errorf("valid metric type %s not recognized", validType)
		}
	}

	for _, invalidType := range invalidTypes {
		metric := MetricType(invalidType)
		switch metric {
		case MetricTypeCPU, MetricTypeMemory, MetricTypeHTTP, MetricTypeConnections, MetricTypeInstanceCount, MetricTypeHTTPErrors, MetricTypeResponseTime:
			t.Errorf("invalid metric type %s was accepted", invalidType)
		default:
			// Invalid - test passes
		}
	}
}
