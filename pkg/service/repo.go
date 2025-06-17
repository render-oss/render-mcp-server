package service

import (
	"context"

	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/pointers"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/validate"
)

//go:generate go tool counterfeiter -o ../fakes/fakeservicerepoclient_gen.go . serviceRepoClient
type serviceRepoClient interface {
	ListServicesWithResponse(ctx context.Context, params *client.ListServicesParams, reqEditors ...client.RequestEditorFn) (*client.ListServicesResponse, error)
	GetEnvVarsForServiceWithResponse(ctx context.Context, serviceId string, params *client.GetEnvVarsForServiceParams, reqEditors ...client.RequestEditorFn) (*client.GetEnvVarsForServiceResponse, error)
	UpdateEnvVarsForServiceWithResponse(ctx context.Context, serviceId string, body []client.EnvVarInput, reqEditors ...client.RequestEditorFn) (*client.UpdateEnvVarsForServiceResponse, error)
	CreateDeployWithResponse(ctx context.Context, serviceId string, body client.CreateDeployJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateDeployResponse, error)
	CreateServiceWithResponse(ctx context.Context, data client.CreateServiceJSONRequestBody, reqEditors ...client.RequestEditorFn) (*client.CreateServiceResponse, error)
	RetrieveServiceWithResponse(ctx context.Context, id string, reqEditors ...client.RequestEditorFn) (*client.RetrieveServiceResponse, error)
}

type Repo struct {
	client serviceRepoClient
}

func NewRepo(c serviceRepoClient) *Repo {
	return &Repo{
		client: c,
	}
}

func (s *Repo) ListServices(ctx context.Context, params *client.ListServicesParams) ([]*client.Service, error) {
	workspace, err := session.FromContext(ctx).GetWorkspace()
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

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, nil, err
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return nil, nil, nil
	}

	res := *resp.JSON200
	services := make([]*client.Service, 0, len(res))
	for _, serviceWithCursor := range res {
		services = append(services, &serviceWithCursor.Service)
	}

	return services, &res[len(res)-1].Cursor, nil
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

	if err := client.ErrorFromResponse(resp); err != nil {
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

func (s *Repo) UpdateEnvVars(ctx context.Context, serviceId string, envVars []client.EnvVarInput) (*client.UpdateEnvVarsForServiceResponse, error) {
	// validate that the service belongs to the workspace
	_, err := s.GetService(ctx, serviceId)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.UpdateEnvVarsForServiceWithResponse(ctx, serviceId, envVars)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *Repo) DeployService(ctx context.Context, serviceId string) (*client.CreateDeployResponse, error) {
	// Skip validation of the service belongs to the workspace because it should be done before the
	// call to DeployService.
	resp, err := s.client.CreateDeployWithResponse(ctx, serviceId, client.CreateDeployJSONRequestBody{
		ClearCache: nil,
		CommitId:   nil,
		ImageUrl:   nil,
	})
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
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

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON201, nil
}

func (s *Repo) GetService(ctx context.Context, id string) (*client.Service, error) {
	resp, err := s.client.RetrieveServiceWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := client.ErrorFromResponse(resp); err != nil {
		return nil, err
	}

	return resp.JSON200, nil
}
