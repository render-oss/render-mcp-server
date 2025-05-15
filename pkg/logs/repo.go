package logs

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
)

func NewLogRepo(c *client.ClientWithResponses) *LogRepo {
	return &LogRepo{c: c}
}

type LogRepo struct {
	c *client.ClientWithResponses
}

func (l *LogRepo) ListLogs(ctx context.Context, params *client.ListLogsParams) (*client.Logs200Response, error) {
	logs, err := l.c.ListLogsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(logs); err != nil {
		return nil, err
	}

	return logs.JSON200, nil
}

func (l *LogRepo) ListLogLabelValues(ctx context.Context, params *client.ListLogsValuesParams) ([]string, error) {
	resp, err := l.c.ListLogsValuesWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return *resp.JSON200, nil
}
