package byteutil

import (
	"bytes"
	"encoding/json"
)

func PrettyPrintJSON(jsonByteArray []byte) (string, error) {
	var prettyJSON bytes.Buffer
	err := json.Indent(&prettyJSON, jsonByteArray, "", "  ")
	if err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}
