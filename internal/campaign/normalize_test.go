package campaign

import "testing"

func TestTargetDigestChangesWithCommand(t *testing.T) {
	first, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Target.Command = append([]string(nil), first.Target.Command...)
	second.Target.Command[0] = "other-go"
	left, err := first.TargetDigest()
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.TargetDigest()
	if err != nil {
		t.Fatal(err)
	}
	if left == right || len(left) != len("sha256:")+64 {
		t.Fatalf("target digest = %q, %q", left, right)
	}
}
