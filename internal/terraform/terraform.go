package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/hashicorp/terraform-exec/tfexec"
	"log"
	"os/exec"
	"strings"
	"time"
)

const (
	CHANGE_SUMMARY_TYPE  = "change_summary"
	TERRAFORM_ERROR_TYPE = "error"
)

type OperationType string

const (
	OperationTypePlan OperationType = "plan"
	OperationTypeInit OperationType = "init"
)

type TerraformOperation struct {
	DirectoryPath string
}

type TerraformPlanSummary struct {
	Add    int
	Change int
	Remove int
}

type TerraformOuput struct {
	Level      string    `json:"@level"`
	Message    string    `json:"@message"`
	Module     string    `json:"@module"`
	Timestamp  time.Time `json:"@timestamp"`
	Terraform  string    `json:"terraform,omitempty"`
	Type       string    `json:"type"`
	UI         string    `json:"ui,omitempty"`
	Diagnostic struct {
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
		Detail   string `json:"detail"`
	} `json:"diagnostic,omitempty"`
	Changes struct {
		Add       int    `json:"add"`
		Change    int    `json:"change"`
		Import    int    `json:"import"`
		Remove    int    `json:"remove"`
		Operation string `json:"operation"`
	} `json:"changes,omitempty"`
}

type TerraformOperationInterface interface {
	ExecuteTerraformPlan(ctx context.Context) (*TerraformPlanSummary, error)
	ExecuteTerraformInit(ctx context.Context) error
	SelectWorkspace(ctx context.Context, workspace string) error
	StatePull(ctx context.Context) ([]byte, error)
}

func (tOp *TerraformOperation) ExecuteTerraformPlan(ctx context.Context) (*TerraformPlanSummary, error) {
	var buffer, errBuffer bytes.Buffer
	var terraformOutputLine TerraformOuput
	var errString string

	cmd := exec.Command("terraform", "plan", "-json")
	cmd.Dir = tOp.DirectoryPath
	cmd.Stdout = &buffer
	cmd.Stderr = &errBuffer
	err := cmd.Run()
	terraformOutputs := parseTerraformOutput(buffer)

	if err != nil {
		for _, terraformOutput := range terraformOutputs {
			if terraformOutput.Level == TERRAFORM_ERROR_TYPE {
				errString = errString + "\n" + terraformOutput.Message
			}
		}
		return nil, errors.New(errString)
	}

	for _, terraformOutput := range terraformOutputs {
		if terraformOutput.Type == CHANGE_SUMMARY_TYPE {
			terraformOutputLine = terraformOutput
			break
		}
	}

	return &TerraformPlanSummary{
		Add:    terraformOutputLine.Changes.Add,
		Change: terraformOutputLine.Changes.Change,
		Remove: terraformOutputLine.Changes.Remove,
	}, nil
}

func (tOp *TerraformOperation) ExecuteTerraformInit(ctx context.Context) error {
	var buffer, errBuffer bytes.Buffer

	cmd := exec.Command("terraform", "init", "-no-color")
	cmd.Dir = tOp.DirectoryPath
	cmd.Stdout = &buffer
	cmd.Stderr = &errBuffer
	err := cmd.Run()

	if err != nil {
		return errors.New(errBuffer.String())
	}

	return nil
}

func (tOp *TerraformOperation) SelectWorkspace(ctx context.Context, workspace string) error {
	var buffer, errBuffer bytes.Buffer

	cmd := exec.Command("terraform", "workspace", "select", workspace, "-no-color")
	cmd.Dir = tOp.DirectoryPath
	cmd.Stdout = &buffer
	cmd.Stderr = &errBuffer
	err := cmd.Run()

	if err != nil {
		return errors.New(errBuffer.String())
	}
	return nil
}

func (tOp *TerraformOperation) StatePull(ctx context.Context) ([]byte, error) {
	tf, err := tfexec.NewTerraform(tOp.DirectoryPath, "terraform")
	if err != nil {
		log.Fatalf("error running NewTerraform: %s", err)
		return nil, err
	}
	res, err := tf.StatePull(ctx)
	return []byte(res), nil
}

func parseTerraformOutput(buffer bytes.Buffer) []TerraformOuput {

	var terraformOutputs []TerraformOuput

	if buffer.Len() > 0 {
		for _, line := range strings.Split(buffer.String(), "\n") {
			var terraformOutputLine TerraformOuput
			if line == "" {
				continue
			}
			err := json.Unmarshal([]byte(line), &terraformOutputLine)
			if err != nil {
				return nil
			}
			terraformOutputs = append(terraformOutputs, terraformOutputLine)
		}
	}
	return terraformOutputs
}
