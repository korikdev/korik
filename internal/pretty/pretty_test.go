package pretty

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPrettyPrintJSON_SortedKeys(t *testing.T) {
	data := map[string]interface{}{
		"zebra": 1,
		"alpha": 2,
		"mango": 3,
	}
	out, err := PrettyPrintJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := "{\n  \"alpha\": 2,\n  \"mango\": 3,\n  \"zebra\": 1\n}"
	if string(out) != expected {
		t.Errorf("expected:\n%s\n\ngot:\n%s", expected, string(out))
	}
}

func TestRoundTrip(t *testing.T) {
	input := []byte(`{"zebra":1,"alpha":{"nested":true,"beta":false},"mango":[1,2,3]}`)
	out, err := RoundTrip(input)
	if err != nil {
		t.Fatal(err)
	}
	var v1, v2 interface{}
	if err := json.Unmarshal(input, &v1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &v2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v1, v2) {
		t.Errorf("round-trip changed structure: %v != %v", v1, v2)
	}
}

func TestDifferentFormattingSameOutput(t *testing.T) {
	inputs := []string{
		`{"a":1,"b":2}`,
		`{"b":2,"a":1}`,
		`{"a": 1, "b": 2}`,
		`{ "a" : 1 , "b" : 2 }`,
	}
	var results [][]byte
	for _, in := range inputs {
		out, err := PrettyPrintBytes([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, out)
	}
	for i := 1; i < len(results); i++ {
		if string(results[0]) != string(results[i]) {
			t.Errorf("different formatting produced different output:\n%s\nvs\n%s", results[0], results[i])
		}
	}
}

func TestInvalidJSONRejected(t *testing.T) {
	_, err := PrettyPrintBytes([]byte(`{not valid json}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	_, err = SortKeys([]byte(`{bad}`))
	if err == nil {
		t.Error("expected error for invalid JSON in SortKeys")
	}
	err = ValidateConfig([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON in ValidateConfig")
	}
}

func TestSortKeys(t *testing.T) {
	input := []byte(`{"z":1,"a":2,"m":3}`)
	out, err := SortKeys(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := "{\n  \"a\": 2,\n  \"m\": 3,\n  \"z\": 1\n}"
	if string(out) != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, string(out))
	}
}

func TestSaveAndLoadPretty(t *testing.T) {
	data := map[string]interface{}{"z": 1, "a": 2}
	err := SavePretty("/tmp/test_pretty.json", data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := LoadAndPretty("/tmp/test_pretty.json")
	if err != nil {
		t.Fatal(err)
	}
	var v1, v2 interface{}
	b, _ := json.Marshal(data)
	json.Unmarshal(b, &v1)
	json.Unmarshal(out, &v2)
	if !reflect.DeepEqual(v1, v2) {
		t.Errorf("save/load changed data: %v != %v", v1, v2)
	}
}

func TestRoundTripPreservesStructure(t *testing.T) {
	input := []byte(`{"a":1,"b":{"d":4,"c":3},"c":[1,2]}`)
	out, err := RoundTrip(input)
	if err != nil {
		t.Fatal(err)
	}
	var original, roundtripped interface{}
	json.Unmarshal(input, &original)
	json.Unmarshal(out, &roundtripped)
	if !reflect.DeepEqual(original, roundtripped) {
		t.Errorf("structure changed: %v != %v", original, roundtripped)
	}
}
