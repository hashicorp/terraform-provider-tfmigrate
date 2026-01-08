package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	httpUtil "terraform-provider-tfmigrate/internal/util/net"
	tfeUtil "terraform-provider-tfmigrate/internal/util/tfe"

	"github.com/hashicorp/go-tfe"
	"github.com/vmihailenco/msgpack/v5"
)

type StackDeploymentGroupData struct {
	Id     string                    `json:"id,omitempty"`     // Id of the stack deployment group.
	Status tfe.DeploymentGroupStatus `json:"status,omitempty"` // Status of the stack deployment group.
}
type StackMigrationTrackRequest struct {
	MigrationMap map[string]string // MigrationMap is a map of workspace names to deployment names.
	OrgName      string            // OrgName is the name of the TFE organization.
	StackId      string            // StackId is the ID of the stack to which deployments belong.
}

type StackMigrationData struct {
	WorkspaceId         string                   `json:"workspace_id,omitempty"`          // WorkspaceId is the ID of a TFE workspace to be migrated.
	DeploymentName      string                   `json:"deployment_name,omitempty"`       // DeploymentName is the name of the deployment in the TFE workspace.
	DeploymentGroupData StackDeploymentGroupData `json:"deployment_group_data,omitempty"` // DeploymentGroupData contains the deployment group data for the workspace.
	FailureReason       string                   `json:"failure_reason,omitempty"`        // FailureReason is the failure reason of the stack deployment group, if any.
	Warnings            []string                 `json:"warnings,omitempty"`              // Warnings is a list of warnings for the stack deployment group, if any.
}

type stackMigrationHashService struct {
	config     *tfe.Config
	ctx        context.Context
	httpClient httpUtil.Client
	tfeClient  *tfe.Client
	tfeUtil    tfeUtil.TfeUtil
}

type StackMigrationHashService interface {
	GenerateMigrationData(request StackMigrationTrackRequest) (map[string]StackMigrationData, error)
	GetMigrationData(migrationHash string) (map[string]StackMigrationData, error)
	GetMigrationHash(migrationData map[string]StackMigrationData) (string, error)
	UpdateContext(ctx context.Context)
}

func NewStackMigrationHashService(ctx context.Context, tfeUtil tfeUtil.TfeUtil, tfeConfig *tfe.Config, tfeClient *tfe.Client, httpClient httpUtil.Client) StackMigrationHashService {
	return &stackMigrationHashService{
		ctx:        ctx,
		tfeUtil:    tfeUtil,
		tfeClient:  tfeClient,
		config:     tfeConfig,
		httpClient: httpClient,
	}
}

func (s *stackMigrationHashService) GenerateMigrationData(request StackMigrationTrackRequest) (map[string]StackMigrationData, error) {

	if err := s.validateStackMigrationTrackingRequest(request); err != nil {
		return nil, err
	}

	// This is useful during the create operation tfmigrate_stack_migration
	if request.StackId == "" {
		return nil, nil
	}

	migrationData := make(map[string]StackMigrationData)

	return s.getDeploymentDataFromMigrationMap(request, migrationData)

}

func (s *stackMigrationHashService) GetMigrationData(migrationHash string) (map[string]StackMigrationData, error) {
	if migrationHash == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(migrationHash)
	if err != nil {
		return nil, err
	}

	var m map[string]StackMigrationData
	if err := msgpack.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	return m, nil
}

func (s *stackMigrationHashService) GetMigrationHash(migrationData map[string]StackMigrationData) (string, error) {

	if len(migrationData) == 0 {
		return "", nil
	}

	b, err := msgpack.Marshal(migrationData)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (s *stackMigrationHashService) UpdateContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *stackMigrationHashService) getDeploymentDataFromMigrationMap(request StackMigrationTrackRequest, migrationData map[string]StackMigrationData) (map[string]StackMigrationData, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for workspaceName, deploymentName := range request.MigrationMap {
		wg.Add(1)
		go func(workspaceName, deploymentName string) {
			defer wg.Done()
			data := s.getMigrationDataForEachWorkspaceDeploymentPair(request, workspaceName, deploymentName)
			mu.Lock()
			migrationData[workspaceName] = data
			mu.Unlock()
		}(workspaceName, deploymentName)
	}
	wg.Wait()
	return migrationData, nil
}

func (s *stackMigrationHashService) getMigrationDataForEachWorkspaceDeploymentPair(request StackMigrationTrackRequest, workspaceName string, deploymentName string) StackMigrationData {

	// 1. Read the workspace by name to validate the workspace received in the migration map exists in TFE.
	data := StackMigrationData{}
	workspace, err := s.tfeUtil.ReadWorkspaceByName(request.OrgName, workspaceName, s.tfeClient)
	if err != nil {
		data.FailureReason = fmt.Sprintf("Error reading workspace name: %s, error: %v", workspaceName, err)
		return data
	}

	data.WorkspaceId = workspace.ID

	// 2. Read the stack deployment run by the deployment name and stack id to validate the deployment received with the corresponding workspace exists in TFE.
	deploymentRun, err := s.tfeUtil.ReadLatestDeploymentRunByDeploymentName(request.StackId, deploymentName, s.httpClient, s.config, s.tfeClient)
	if err != nil || deploymentRun == nil {
		data.FailureReason = fmt.Sprintf("Error reading deployment data name: %s, error: %v", deploymentName, err)
		return data
	}

	if deploymentRun.ID == "" {
		data.FailureReason = fmt.Sprintf("No deployment with the name %s found in stack", deploymentName)
		return data
	}

	data.DeploymentName = deploymentName
	data.DeploymentGroupData = StackDeploymentGroupData{
		Id:     deploymentRun.StackDeploymentGroup.ID,
		Status: deploymentRun.StackDeploymentGroup.Status,
	}

	return data
}

func (s *stackMigrationHashService) validateStackMigrationTrackingRequest(request StackMigrationTrackRequest) error {
	if request.OrgName == "" {
		return fmt.Errorf("organization name is required")
	}

	if len(request.MigrationMap) == 0 {
		return fmt.Errorf("migration map cannot be empty")
	}
	return nil
}
