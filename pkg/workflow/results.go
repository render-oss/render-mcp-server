package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	workflowclient "github.com/render-oss/render-mcp-server/pkg/client/workflows"
)

const maxResultPageBytes = 32 << 10
const maxErrorBytes = 4 << 10

type resultPage struct {
	TaskRunID string                       `json:"taskRunId"`
	Status    workflowclient.TaskRunStatus `json:"status"`
	Attempt   *int                         `json:"attempt,omitempty"`
	resultContent
}
type flowResultPage struct {
	FlowRunID string                       `json:"flowRunId"`
	Status    workflowclient.FlowRunStatus `json:"status"`
	resultContent
}
type resultContent struct {
	Ready      bool   `json:"ready"`
	Content    string `json:"content"`
	Offset     int    `json:"offset"`
	NextOffset *int   `json:"nextOffset"`
	TotalBytes int    `json:"totalBytes"`
	SHA256     string `json:"sha256,omitempty"`
}

func terminal(status workflowclient.TaskRunStatus) bool {
	switch status {
	case workflowclient.Completed, workflowclient.Succeeded, workflowclient.Failed, workflowclient.Canceled:
		return true
	}
	return false
}
func flowTerminal(status workflowclient.FlowRunStatus) bool {
	switch status {
	case workflowclient.FlowRunStatusSucceeded, workflowclient.FlowRunStatusFailed, workflowclient.FlowRunStatusCanceled:
		return true
	}
	return false
}

func pageResults(run *runDetails, attempt *int, offset, limit int, expectedHash string) (*resultPage, error) {
	raw := run.Results
	status := run.Status
	if attempt != nil {
		found := false
		for _, a := range run.Attempts {
			if a.Attempt == *attempt {
				found = true
				raw = a.Results
				status = a.Status
				break
			}
		}
		if !found {
			return nil, errors.New("attempt not found in this run; use get_workflow_run to inspect attempt numbers")
		}
	}
	content, err := pageContent(raw, terminal(status), offset, limit, expectedHash)
	if err != nil {
		return nil, err
	}
	return &resultPage{TaskRunID: run.Id, Status: status, Attempt: attempt, resultContent: content}, nil
}
func pageFlowResults(run *flowRunDetails, offset, limit int, expectedHash string) (*flowResultPage, error) {
	content, err := pageContent(run.Results, flowTerminal(run.Status), offset, limit, expectedHash)
	if err != nil {
		return nil, err
	}
	return &flowResultPage{FlowRunID: run.Id, Status: run.Status, resultContent: content}, nil
}

func pageContent(raw json.RawMessage, isTerminal bool, offset, limit int, expectedHash string) (resultContent, error) {
	page := resultContent{Offset: offset}
	if !isTerminal {
		if offset != 0 || expectedHash != "" {
			return page, errors.New("results are not terminal; retry later from offset 0")
		}
		return page, nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	// Compact once per request without decoding numbers or unwrapping the array.
	var compact bytes.Buffer
	if !utf8.Valid(raw) || json.Compact(&compact, raw) != nil {
		return page, errors.New("Workflows API returned invalid result JSON")
	}
	data := compact.Bytes()
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if expectedHash != "" && expectedHash != hash {
		return page, errors.New("results changed between pages; restart at offset 0")
	}
	if offset < 0 || offset > len(data) || (offset < len(data) && !utf8.RuneStart(data[offset])) {
		return page, errors.New("offset must be a UTF-8 boundary within the result")
	}
	if offset > 0 && expectedHash == "" {
		return page, errors.New("sha256 from the first page is required when offset is greater than zero")
	}
	end := min(offset+limit, len(data))
	for end < len(data) && !utf8.RuneStart(data[end]) {
		end--
	}
	if end == offset && offset < len(data) {
		return page, errors.New("limit is too small for the next UTF-8 character")
	}
	page.Ready = true
	page.Content = string(data[offset:end])
	page.TotalBytes = len(data)
	page.SHA256 = hash
	if end < len(data) {
		page.NextOffset = &end
	}
	return page, nil
}

func boundError(value *string) (*string, bool) {
	if value == nil || len(*value) <= maxErrorBytes {
		return value, false
	}
	end := maxErrorBytes
	for !utf8.RuneStart((*value)[end]) {
		end--
	}
	s := (*value)[:end]
	return &s, true
}
func runMetadata(run *runDetails) Run {
	out := run.Run
	out.Error, out.ErrorTruncated = boundError(out.Error)
	for i := range out.Attempts {
		out.Attempts[i].Error, out.Attempts[i].ErrorTruncated = boundError(out.Attempts[i].Error)
	}
	return out
}
func flowRunMetadata(run *flowRunDetails) FlowRun {
	out := run.FlowRun
	out.Error, out.ErrorTruncated = boundError(out.Error)
	for i := range out.Attempts {
		out.Attempts[i].Error, out.Attempts[i].ErrorTruncated = boundError(out.Attempts[i].Error)
	}
	return out
}
