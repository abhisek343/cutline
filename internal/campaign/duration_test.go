package campaign

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCampaignJSONDurationRoundTrip(t *testing.T) {
	spec, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	spec.Exploration.ScheduleTimeout = Duration(30 * time.Second)
	spec.Exploration.DrainTimeout = Duration(150 * time.Millisecond)
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Campaign
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Exploration.ScheduleTimeout != spec.Exploration.ScheduleTimeout || decoded.Exploration.DrainTimeout != spec.Exploration.DrainTimeout {
		t.Fatalf("duration roundtrip changed values: %#v", decoded.Exploration)
	}
	for _, value := range []string{"null", "123", "\"garbage\""} {
		var duration Duration
		if err := json.Unmarshal([]byte(value), &duration); err == nil {
			t.Fatalf("accepted invalid duration %s", value)
		}
	}
}
