package authz

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
)

// The decision snapshot is the evaluator's answer to a broad, fixed set of
// grants and checks (decision_snapshot_cases_test.go), committed in
// testdata/decision_snapshot.json. A change that reshapes how decisions are
// made, without meaning to change any, must reproduce it exactly. A change
// that means to change answers regenerates it with
//
//	go test ./internal/authz -run TestDecisionSnapshot -args -write-decision-snapshot
//
// and the diff of the file is the list of answers it changes.

const decisionSnapshotPath = "testdata/decision_snapshot.json"

var writeDecisionSnapshot = flag.Bool("write-decision-snapshot", false, "regenerate "+decisionSnapshotPath)

func TestDecisionSnapshot(t *testing.T) {
	t.Parallel()
	if *writeDecisionSnapshot {
		data, err := encodeDecisionSnapshot(buildDecisionSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(decisionSnapshotPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	data, err := os.ReadFile(decisionSnapshotPath)
	if err != nil {
		t.Fatalf("read %s: %v", decisionSnapshotPath, err)
	}
	var snapshot snapshotFile
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode %s: %v", decisionSnapshotPath, err)
	}

	changed := 0
	for _, group := range snapshot.Groups {
		for _, set := range group.Sets {
			grants := make([]Grant, len(set.Grants))
			for i, g := range set.Grants {
				grants[i] = snapshot.Grants[g].build()
			}
			for i, c := range group.Checks {
				want := set.Answers[i]
				if got := snapshotAnswer(grants, snapshot.Checks[c].build()); got != want {
					changed++
					if changed <= 20 {
						t.Errorf("%s: %s asked %s answers %c, snapshot says %c", group.Name, describeSnapshotGrants(snapshot, set.Grants), describeSnapshotCheck(snapshot.Checks[c]), got, want)
					}
				}
			}
		}
	}
	if changed > 20 {
		t.Errorf("... %d answers changed in all", changed)
	}
}

func describeSnapshotGrants(snapshot snapshotFile, set []int) string {
	if len(set) == 0 {
		return "[no grants]"
	}
	out := "["
	for i, g := range set {
		if i > 0 {
			out += ", "
		}
		grant := snapshot.Grants[g]
		out += fmt.Sprintf("%s %s {%s}", grant.Principal, grant.Scope, describeSelectorKey(grant.Selector))
		if grant.Inferred {
			out += " inferred"
		}
	}
	return out + "]"
}

func describeSnapshotCheck(c snapshotCheck) string {
	data, _ := json.Marshal(c)
	return string(data)
}

// encodeDecisionSnapshot writes one grant, check or grant set per line, so a
// change to the answers shows as a readable diff.
func encodeDecisionSnapshot(snapshot snapshotFile) ([]byte, error) {
	var out bytes.Buffer
	line := func(prefix string, v any, last bool) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out.WriteString(prefix)
		out.Write(data)
		if !last {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
		return nil
	}
	out.WriteString("{\n\"grants\": [\n")
	for i, g := range snapshot.Grants {
		if err := line("  ", g, i == len(snapshot.Grants)-1); err != nil {
			return nil, err
		}
	}
	out.WriteString("],\n\"checks\": [\n")
	for i, c := range snapshot.Checks {
		if err := line("  ", c, i == len(snapshot.Checks)-1); err != nil {
			return nil, err
		}
	}
	out.WriteString("],\n\"groups\": [\n")
	for i, group := range snapshot.Groups {
		header, err := json.Marshal(struct {
			Name   string `json:"name"`
			Checks []int  `json:"checks"`
		}{group.Name, group.Checks})
		if err != nil {
			return nil, err
		}
		out.Write(header[:len(header)-1])
		out.WriteString(",\"sets\": [\n")
		for j, set := range group.Sets {
			if err := line("    ", set, j == len(group.Sets)-1); err != nil {
				return nil, err
			}
		}
		out.WriteString("]}")
		if i < len(snapshot.Groups)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("]\n}\n")
	return out.Bytes(), nil
}
