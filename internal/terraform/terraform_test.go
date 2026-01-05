// Copyright IBM Corp. 2024, 2025
// SPDX-License-Identifier: MPL-2.0

package terraform

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

var (
	time1str            = "2024-06-18T19:11:57.543816+05:30"
	time2str            = "2024-06-18T20:09:50.804812+05:30"
	time1, _            = time.Parse(time.RFC3339, time1str)
	time2, _            = time.Parse(time.RFC3339, time2str)
	validTfOutputString = `
{"@level":"info","@message":"Terraform 1.7.0","@module":"terraform.ui","@timestamp":"` + time1str + `","terraform":"1.7.0","type":"version","ui":"1.2"}
{"@level":"info","@message":"Plan: 2 to add, 0 to change, 0 to destroy.","@module":"terraform.ui","@timestamp":"` + time2str + `","changes":{"add":2,"change":0,"import":0,"remove":0,"operation":"plan"},"type":"change_summary"}
`
	validTfOutputParsed = []TerraformOuput{
		{
			Level:     "info",
			Message:   "Terraform 1.7.0",
			Module:    "terraform.ui",
			Timestamp: time1,
			Terraform: "1.7.0",
			Type:      "version",
			UI:        "1.2",
		},
		{
			Level:     "info",
			Message:   "Plan: 2 to add, 0 to change, 0 to destroy.",
			Module:    "terraform.ui",
			Timestamp: time2,
			Type:      "change_summary",
			Changes: struct {
				Add       int    `json:"add"`
				Change    int    `json:"change"`
				Import    int    `json:"import"`
				Remove    int    `json:"remove"`
				Operation string `json:"operation"`
			}{
				Add:       2,
				Change:    0,
				Import:    0,
				Remove:    0,
				Operation: "plan",
			},
		},
	}
	invalidTfOutputString = `dsfbdbj`
)

func Test_parseTerraformOutput(t *testing.T) {
	tests := []struct {
		name           string
		stringTfOutput string
		parsedTfOutput []TerraformOuput
	}{
		{name: "VALID", stringTfOutput: validTfOutputString, parsedTfOutput: validTfOutputParsed},
		{name: "INVALID", stringTfOutput: invalidTfOutputString, parsedTfOutput: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var buffer bytes.Buffer
			writeToBuffer(&buffer, tt.stringTfOutput)

			if got := parseTerraformOutput(buffer); !reflect.DeepEqual(got, tt.parsedTfOutput) {
				t.Errorf("parseTerraformOutput() = %v, want %v", got, tt.parsedTfOutput)
			}
		})
	}
}

func writeToBuffer(buffer *bytes.Buffer, data string) {
	buffer.WriteString(data)
}
