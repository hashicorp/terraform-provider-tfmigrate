package byteutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrettyPrintJSON(t *testing.T) {
	tests := []struct {
		name           string
		input          []byte
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "Valid JSON",
			input:          []byte(`{"name":"John","age":30,"city":"New York"}`),
			expectedOutput: "{\n  \"name\": \"John\",\n  \"age\": 30,\n  \"city\": \"New York\"\n}",
			expectError:    false,
		},
		{
			name:           "Invalid JSON",
			input:          []byte(`{"name":"John","age":30,"city":"New York"`),
			expectedOutput: "",
			expectError:    true,
		},
		{
			name:           "Empty JSON",
			input:          []byte(`{}`),
			expectedOutput: "{}",
			expectError:    false,
		},
		{
			name:           "Nested JSON",
			input:          []byte(`{"name":"John","address":{"city":"New York","zip":"10001"}}`),
			expectedOutput: "{\n  \"name\": \"John\",\n  \"address\": {\n    \"city\": \"New York\",\n    \"zip\": \"10001\"\n  }\n}",
			expectError:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			output, err := PrettyPrintJSON(tc.input)
			if tc.expectError {
				r.Error(err)
			} else {
				r.NoError(err)
				r.Equal(tc.expectedOutput, output)
			}
		})
	}
}
