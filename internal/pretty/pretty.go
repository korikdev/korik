package pretty

import (
	"encoding/json"
	"fmt"
	"os"
)

const indent = "  "

func PrettyPrintJSON(data interface{}) ([]byte, error) {
	return json.MarshalIndent(data, "", indent)
}

func PrettyPrintBytes(data []byte) ([]byte, error) {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return json.MarshalIndent(raw, "", indent)
}

func LoadAndPretty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return PrettyPrintBytes(data)
}

func SavePretty(path string, data interface{}) error {
	pretty, err := PrettyPrintJSON(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pretty, 0600)
}

func RoundTrip(data []byte) ([]byte, error) {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}
	pretty, err := json.MarshalIndent(raw, "", indent)
	if err != nil {
		return nil, fmt.Errorf("marshal failed: %w", err)
	}
	var raw2 interface{}
	if err := json.Unmarshal(pretty, &raw2); err != nil {
		return nil, fmt.Errorf("re-parse failed: %w", err)
	}
	b1, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("canonical marshal original failed: %w", err)
	}
	b2, err := json.Marshal(raw2)
	if err != nil {
		return nil, fmt.Errorf("canonical marshal roundtrip failed: %w", err)
	}
	if string(b1) != string(b2) {
		return nil, fmt.Errorf("round-trip mismatch")
	}
	return pretty, nil
}

func SortKeys(raw []byte) ([]byte, error) {
	var rawVal interface{}
	if err := json.Unmarshal(raw, &rawVal); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return json.MarshalIndent(rawVal, "", indent)
}

func ValidateConfig(data []byte) error {
	var raw interface{}
	return json.Unmarshal(data, &raw)
}
