package tracker

import "testing"

func TestTrackerTimeUnmarshalJSON_Formats(t *testing.T) {
	tests := []string{
		`2025-12-19T02:02:43.196+0000`,
		`2025-12-19T02:02:43+0000`,
		`2025-12-19T02:02:43Z`,
		`2025-12-19T02:02:43.196Z`,
	}

	for _, s := range tests {
		var tt TrackerTime
		data := []byte("\"" + s + "\"")
		if err := tt.UnmarshalJSON(data); err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		if tt.IsZero() {
			t.Fatalf("parsed time is zero for %s", s)
		}
	}
}

func TestTrackerTime_UnmarshalEmptyAndNull(t *testing.T) {
	var tt TrackerTime
	if err := tt.UnmarshalJSON([]byte(`""`)); err != nil {
		t.Fatal(err)
	}
	if !tt.IsZero() {
		t.Fatal("expected zero time for empty string")
	}

	var tt2 TrackerTime
	if err := tt2.UnmarshalJSON([]byte(`null`)); err != nil {
		t.Fatal(err)
	}
	if !tt2.IsZero() {
		t.Fatal("expected zero time for null")
	}
}

func TestTrackerTime_UnmarshalInvalid(t *testing.T) {
	var tt TrackerTime
	if err := tt.UnmarshalJSON([]byte(`"not-a-time"`)); err == nil {
		t.Fatal("expected error for invalid time")
	}
}
