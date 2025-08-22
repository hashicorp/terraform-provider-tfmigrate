package models

import (
	"time"

	"github.com/hashicorp/go-tfe"
)

type StackDiagnostic struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Severity  string                `json:"severity"`
		Summary   string                `json:"summary"`
		Detail    string                `json:"detail"`
		Diags     []tfe.StackDiagnostic `json:"diags"`
		CreatedAt time.Time             `json:"created-at"`
	} `json:"attributes"`
}

type StackConfigDiagnostics struct {
	Data []StackDiagnostic `json:"data"`
}
