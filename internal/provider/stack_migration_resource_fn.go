package provider

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/vmihailenco/msgpack/v5"
)

var _ function.Function = &DecodeStacksMigrationHashToJson{}

func NewDecodeStackMigrationHashToJson() function.Function {
	return &DecodeStacksMigrationHashToJson{}
}

type DecodeStacksMigrationHashToJson struct{}

func (d *DecodeStacksMigrationHashToJson) Metadata(ctx context.Context, request function.MetadataRequest, response *function.MetadataResponse) {
	response.Name = "decode_stacks_migration_hash_to_json"
}

func (d *DecodeStacksMigrationHashToJson) Definition(ctx context.Context, request function.DefinitionRequest, response *function.DefinitionResponse) {
	response.Definition.Summary = "Decodes a migration hash string of stack migration into a JSON object."
	response.Definition.Description = "This function takes a migration hash string of stack migration as input and decodes it into a structured JSON object for easier processing."

	response.Definition.Parameters = []function.Parameter{
		function.StringParameter{
			Name:        "migration_hash",
			Description: "The migration hash string to be decoded.",
		},
	}

	response.Definition.Return = function.StringReturn{}
}

func (d *DecodeStacksMigrationHashToJson) Run(ctx context.Context, request function.RunRequest, response *function.RunResponse) {
	var migrationHash *string

	if response.Error = function.ConcatFuncErrors(response.Error, request.Arguments.Get(ctx, &migrationHash)); response.Error != nil {
		return
	}

	// Placeholder for actual decoding logic
	decodedJSON := "{}" // Replace with actual decoded JSON string

	if migrationHash == nil {
		response.Error = function.ConcatFuncErrors(response.Error, response.Result.Set(ctx, decodedJSON))
		return
	}

	if *migrationHash == "" {
		response.Error = function.ConcatFuncErrors(response.Error, response.Result.Set(ctx, decodedJSON))
		return
	}

	data, err := base64.StdEncoding.DecodeString(*migrationHash)
	if err != nil {
		response.Error = function.ConcatFuncErrors(response.Error,
			function.NewFuncError(fmt.Sprintf("failed to decode migration hash: %q", err)))
		return
	}

	var migrationData map[string]StackMigrationData
	if err := msgpack.Unmarshal(data, &migrationData); err != nil {
		response.Error = function.ConcatFuncErrors(response.Error,
			function.NewFuncError(fmt.Sprintf("failed to unmarshal migration data: %q", err)))
	}

	decodedJSON = prettyPrintJSON(migrationData)
	response.Error = function.ConcatFuncErrors(response.Error, response.Result.Set(ctx, decodedJSON))
}
