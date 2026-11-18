package service

import (
	"context"
	"fmt"
	"time"

	"github.com/render-oss/render-mcp-server/pkg/client"
	envvar "github.com/render-oss/render-mcp-server/pkg/client/envvar"
	eventtypes "github.com/render-oss/render-mcp-server/pkg/client/eventtypes"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakeservicerepoclient_gen.go . serviceRepoClient
type serviceRepoClient interface {
	ListServicesWithResponse(ctx context.Context, params *client.ListServicesParams, reqEditors ...client.RequestEditorFn) (*client.ListServicesResponse, error)
	GetEnvVarsForServiceWithResponse(ctx context.Context, serviceId string, params *client.GetEnvVarsForServiceParams, reqEditors ...client.RequestEditorFn) (*client.GetEnvVarsForServiceResponse, error)
	UpdateEnvVarsForServiceWithResponse(ctx context.Context, serviceId string, body []envvar.EnvVarInput, reqEditors ...client.RequestEditorFn) (*client.UpdateEnvVarsForServiceResponse, error)
	CreateServiceWithResponse(ctx context.Context, data client.CreateServiceJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateServiceResponse, error)
	RetrieveServiceWithResponse(ctx context.Context, id string, reqEditors ...client.RequestEditorFn) (*client.RetrieveServiceResponse, error)
	RunCronJobWithResponse(ctx context.Context, cronJobId client.CronJobIdParam, reqEditors ...client.RequestEditorFn) (*client.RunCronJobResponse, error)
	CancelCronJobRunWithResponse(ctx context.Context, cronJobId client.CronJobIdParam, reqEditors ...client.RequestEditorFn) (*client.CancelCronJobRunResponse, error)
	ListEventsWithResponse(ctx context.Context, serviceId client.ServiceIdParam, params *client.ListEventsParams, reqEditors ...client.RequestEditorFn) (*client.ListEventsResponse, error)
}

type Repo struct {
	client serviceRepoClient

	// runRecordedTimeout and runRecordedPollInterval bound how long
	// RunCronJob waits for a new run to show up in the events API.
	runRecordedTimeout      time.Duration
	runRecordedPollInterval time.Duration
}

func NewRepo(c serviceRepoClient) *Repo {
	return &Repo{
		client:                  c,
		runRecordedTimeout:      runRecordedTimeout,
		runRecordedPollInterval: runRecordedPollInterval,
	}
}

func (s *Repo) ListServices(ctx context.Context, params *client.ListServicesParams) ([]*client.Service, error) {
	workspace, err := session.FromContext(ctx).GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if workspace != "" {
		params.OwnerId = pointers.From([]string{workspace})
	}

	return client.ListAll(ctx, params, s.listPage)
}

func (s *Repo) listPage(ctx context.Context, params *client.ListServicesParams) ([]*client.Service, *client.Cursor, error) {
	resp, err := s.client.ListServicesWithResponse(ctx, params)
	if err != nil {
		return nil, nil, err
	}

	body, err := client.BodyFromResponse(ctx, resp.JSON200, resp)
	if err != nil {
		return nil, nil, err
	}

	res := *body
	services := make([]*client.Service, 0, len(res))
	for _, serviceWithCursor := range res {
		services = append(services, &serviceWithCursor.Service)
	}

	var cursor *client.Cursor
	if len(res) > 0 {
		cursor = &res[len(res)-1].Cursor
	}

	return services, cursor, nil
}

type ListEnvParams struct {
	*client.GetEnvVarsForServiceParams
	serviceId string
}

func (s *Repo) ListEnvVars(ctx context.Context, serviceId string, params *client.GetEnvVarsForServiceParams) ([]*client.EnvVar, error) {
	listEnvParams := &ListEnvParams{
		GetEnvVarsForServiceParams: params,
		serviceId:                  serviceId,
	}
	return client.ListAll(ctx, listEnvParams, s.listEnvVarsPage)
}

func (s *Repo) listEnvVarsPage(ctx context.Context, params *ListEnvParams) ([]*client.EnvVar, *client.Cursor, error) {
	resp, err := s.client.GetEnvVarsForServiceWithResponse(ctx, params.serviceId, params.GetEnvVarsForServiceParams)
	if err != nil {
		return nil, nil, err
	}

	if err := client.ErrorFromResponse(ctx, resp); err != nil {
		return nil, nil, err
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return nil, nil, nil
	}

	res := *resp.JSON200
	envVars := make([]*client.EnvVar, 0, len(res))
	for _, envVarsWithCursor := range res {
		envVars = append(envVars, &envVarsWithCursor.EnvVar)
	}

	return envVars, &res[len(res)-1].Cursor, nil
}

func (s *Repo) UpdateEnvVars(ctx context.Context, serviceId string, envVars []envvar.EnvVarInput) (*client.UpdateEnvVarsForServiceResponse, error) {
	// Validate that the service belongs to the workspace in the current
	// session before mutating it.
	service, err := s.GetService(ctx, serviceId)
	if err != nil {
		return nil, err
	}
	if err := validate.WorkspaceMatches(ctx, service.OwnerId); err != nil {
		return nil, err
	}

	resp, err := s.client.UpdateEnvVarsForServiceWithResponse(ctx, serviceId, envVars)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(ctx, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *Repo) CreateService(ctx context.Context, data client.CreateServiceJSONRequestBody) (*client.ServiceAndDeploy, error) {
	if err := validate.WorkspaceMatches(ctx, data.OwnerId); err != nil {
		return nil, err
	}

	resp, err := s.client.CreateServiceWithResponse(ctx, data)
	if err != nil {
		return nil, err
	}

	return client.BodyFromResponse(ctx, resp.JSON201, resp)
}

func (s *Repo) GetService(ctx context.Context, id string) (*client.Service, error) {
	resp, err := s.client.RetrieveServiceWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	return client.BodyFromResponse(ctx, resp.JSON200, resp)
}

// cronJobRunLookback bounds the event query that looks for an in-progress
// run. Render kills a cron job run 12 hours after its pod starts, so a run
// that started earlier than this cannot still be active. The started event is
// stamped with the pod's creation time, which can precede the start of that
// 12-hour clock by however long the pod waits to be scheduled; the extra hour
// covers that and clock skew.
//
// The bound also limits the damage when a run's ended event is never
// persisted, which has happened: such a run looks active, and the tool
// refuses without cancelActiveRun, but only until the window passes it.
const cronJobRunLookback = 13 * time.Hour

// runRecordedTimeout is how long RunCronJob waits for a run it triggered to
// appear in the events API, polling every runRecordedPollInterval. The gap is
// normally a few seconds: pod creation plus the event watcher's queue.
const (
	runRecordedTimeout      = 20 * time.Second
	runRecordedPollInterval = 2 * time.Second
)

// cronJobRunEventLimit bounds the run event query. Only the latest start and any end
// event for it matter, and both are among the newest events.
const cronJobRunEventLimit = 20

// ActiveCronJobRun identifies a run that has started and not yet ended.
type ActiveCronJobRun struct {
	Id        string    `json:"id"`
	StartedAt time.Time `json:"startedAt"`
}

// ActiveCronJobRunError is returned by RunCronJob when a run is in progress
// and the caller did not agree to cancel it.
type ActiveCronJobRunError struct {
	ServiceId string
	Run       ActiveCronJobRun
}

func (e *ActiveCronJobRunError) Error() string {
	return fmt.Sprintf("cron job %s already has a run in progress (run %s, started %s). "+
		"Starting a new run cancels it. Wait for it to finish (list_events with eventTypes "+
		"cron_job_run_ended shows when it does), or, if the user agrees to cancel it, call "+
		"run_cron_job again with cancelActiveRun set to true.",
		e.ServiceId, e.Run.Id, e.Run.StartedAt.UTC().Format(time.RFC3339))
}

// CronJobRunResult is what RunCronJob reports back: the new run, and the run
// it replaced when one was in progress.
type CronJobRunResult struct {
	Run         *client.CronJobRun `json:"run"`
	CanceledRun *ActiveCronJobRun  `json:"canceledRun,omitempty"`
	// Warning is set when the new run had not yet appeared in the events API
	// by the time RunCronJob returned.
	Warning string `json:"warning,omitempty"`
}

// RunCronJob triggers a manual run of a cron job. The API cancels any active
// run when it starts a new one, so unless cancelActiveRun is set, RunCronJob
// refuses with an *ActiveCronJobRunError while a run is in progress.
//
// It fetches the service first so it can check the workspace, the service
// type, and suspension before triggering anything: the API's errors for those
// cases are opaque to the model.
func (s *Repo) RunCronJob(ctx context.Context, serviceId string, cancelActiveRun bool) (*CronJobRunResult, error) {
	if _, err := s.getCronJob(ctx, serviceId); err != nil {
		return nil, err
	}

	active, err := s.activeCronJobRun(ctx, serviceId)
	if err != nil && !cancelActiveRun {
		return nil, fmt.Errorf("could not check whether cron job %s has a run in progress, "+
			"and starting a new run would cancel one: %w. Retry, or, if the user agrees to "+
			"cancel any in-progress run, call run_cron_job again with cancelActiveRun set to true",
			serviceId, err)
	}
	if active != nil && !cancelActiveRun {
		return nil, &ActiveCronJobRunError{ServiceId: serviceId, Run: *active}
	}

	resp, err := s.client.RunCronJobWithResponse(ctx, client.CronJobIdParam(serviceId))
	if err != nil {
		return nil, err
	}
	run, err := client.BodyFromResponse(ctx, resp.JSON200, resp)
	if err != nil {
		return nil, err
	}

	result := &CronJobRunResult{Run: run, CanceledRun: active}
	if !s.waitForRunRecorded(ctx, serviceId, run.Id) {
		result.Warning = fmt.Sprintf("Run %s was triggered but has not been recorded as started "+
			"yet, so run_cron_job cannot see it. Calling run_cron_job for %s again now would "+
			"cancel it. Before starting another run, wait until list_events with eventTypes "+
			"cron_job_run_started shows run %s.", run.Id, serviceId, run.Id)
	}
	return result, nil
}

// waitForRunRecorded polls the events API until a started or ended event for
// runId appears, and reports whether it did before runRecordedTimeout.
//
// The API returns from a trigger once it has created the run's Kubernetes Job,
// but the cron_job_run_started event is only written when the run's pod is
// first seen. Until then activeCronJobRun cannot see the run, and a second
// run_cron_job call would cancel it without asking. Returning only after the
// run is visible closes that gap for the caller's next call.
func (s *Repo) waitForRunRecorded(ctx context.Context, serviceId, runId string) bool {
	ctx, cancel := context.WithTimeout(ctx, s.runRecordedTimeout)
	defer cancel()

	ticker := time.NewTicker(s.runRecordedPollInterval)
	defer ticker.Stop()
	for {
		// A failed poll is retried like an empty one: the run has already
		// been triggered, so the only question left is whether to warn.
		events, err := s.cronJobRunEvents(ctx, serviceId)
		if err == nil {
			for _, e := range events {
				if e.runId == runId {
					return true
				}
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// CancelCronJobRun cancels the in-progress run of a cron job.
func (s *Repo) CancelCronJobRun(ctx context.Context, serviceId string) error {
	if _, err := s.getCronJob(ctx, serviceId); err != nil {
		return err
	}

	resp, err := s.client.CancelCronJobRunWithResponse(ctx, client.CronJobIdParam(serviceId))
	if err != nil {
		return err
	}
	return client.ErrorFromResponse(ctx, resp)
}

// getCronJob fetches a service and checks that it is a cron job in the
// session's workspace that is able to run.
func (s *Repo) getCronJob(ctx context.Context, serviceId string) (*client.Service, error) {
	service, err := s.GetService(ctx, serviceId)
	if err != nil {
		return nil, err
	}
	if err := validate.WorkspaceMatches(ctx, service.OwnerId); err != nil {
		return nil, err
	}
	if service.Type != client.CronJob {
		return nil, fmt.Errorf("service %s is a %s, not a cron job. Cron job runs only apply "+
			"to cron jobs; use trigger_deploy to redeploy other service types", serviceId, service.Type)
	}
	if service.Suspended == client.ServiceSuspendedSuspended {
		return nil, fmt.Errorf("cron job %s is suspended and cannot run. It must be resumed "+
			"in the Render Dashboard first", serviceId)
	}
	return service, nil
}

// activeCronJobRun returns the cron job's in-progress run, or nil if there is
// none. The API cancels any active run when it starts a new one, so at most
// one run is active, and it is the most recently started one. That run is
// active if no cron_job_run_ended event for it has arrived.
func (s *Repo) activeCronJobRun(ctx context.Context, serviceId string) (*ActiveCronJobRun, error) {
	events, err := s.cronJobRunEvents(ctx, serviceId)
	if err != nil {
		return nil, err
	}

	// Events arrive newest first. Ended events for the latest run are newer
	// than its start, so they are seen before it.
	ended := map[string]bool{}
	for _, e := range events {
		if e.ended {
			ended[e.runId] = true
			continue
		}
		if ended[e.runId] {
			return nil, nil
		}
		return &ActiveCronJobRun{Id: e.runId, StartedAt: e.at}, nil
	}
	return nil, nil
}

type cronJobRunEvent struct {
	runId string
	ended bool
	at    time.Time
}

// cronJobRunEvents lists the cron job's run started and ended events from the
// lookback window, newest first.
func (s *Repo) cronJobRunEvents(ctx context.Context, serviceId string) ([]cronJobRunEvent, error) {
	// The API accepts several event types joined by commas in its one `type`
	// param. pkg/event relies on the same encoding, and a test pins it.
	var typeParam client.EventTypeParam
	if err := typeParam.FromExternalRef8ServiceEventType(eventtypes.ServiceEventType(
		string(eventtypes.ServiceEventTypeCronJobRunStarted) + "," +
			string(eventtypes.ServiceEventTypeCronJobRunEnded),
	)); err != nil {
		return nil, err
	}
	params := &client.ListEventsParams{
		Type:      &typeParam,
		StartTime: pointers.From(time.Now().UTC().Add(-cronJobRunLookback)),
		Limit:     pointers.From(cronJobRunEventLimit),
	}

	resp, err := s.client.ListEventsWithResponse(ctx, serviceId, params)
	if err != nil {
		return nil, err
	}
	body, err := client.BodyFromResponse(ctx, resp.JSON200, resp)
	if err != nil {
		return nil, err
	}

	events := make([]cronJobRunEvent, 0, len(*body))
	for _, e := range *body {
		switch e.Event.Type {
		case eventtypes.ServiceEventTypeCronJobRunEnded:
			details, err := e.Event.Details.AsCronJobRunEndedEvent()
			if err != nil {
				return nil, err
			}
			events = append(events, cronJobRunEvent{runId: details.CronJobRunId, ended: true, at: e.Event.Timestamp})
		case eventtypes.ServiceEventTypeCronJobRunStarted:
			details, err := e.Event.Details.AsCronJobRunStartedEvent()
			if err != nil {
				return nil, err
			}
			events = append(events, cronJobRunEvent{runId: details.CronJobRunId, at: e.Event.Timestamp})
		}
	}
	return events, nil
}
