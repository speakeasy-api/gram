package authz

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
)

// The decision snapshot is the evaluator's answer to a broad, fixed set of
// grants and checks (decision_snapshot_cases_test.go), committed in
// testdata/decision_snapshot.json. The test builds the cases again, checks
// that they are exactly the committed ones, and compares every answer. A
// change that reshapes how decisions are made, without meaning to change any,
// must reproduce the file exactly. A change that means to change answers, or
// adds cases, regenerates it from server/ with
//
//	go test ./internal/authz -run TestDecisionSnapshot -args -write-decision-snapshot
//
// and the diff of the file is the list of answers it changes.

func TestDecisionSnapshot(t *testing.T) {
	t.Parallel()
	fresh := buildDecisionSnapshot()
	if *writeDecisionSnapshot {
		data, err := encodeDecisionSnapshot(fresh)
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
	var committed snapshotFile
	if err := json.Unmarshal(data, &committed); err != nil {
		t.Fatalf("decode %s: %v", decisionSnapshotPath, err)
	}
	// Compare as stored: an empty map and a missing one read back the same.
	encoded, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	var built snapshotFile
	if err := json.Unmarshal(encoded, &built); err != nil {
		t.Fatal(err)
	}
	if !sameSnapshotCases(committed, built) {
		t.Fatalf("the cases in %s are not the ones decision_snapshot_cases_test.go builds; regenerate it", decisionSnapshotPath)
	}

	changed := 0
	for g, group := range committed.Groups {
		for s, set := range group.Sets {
			got := built.Groups[g].Sets[s].Answers
			for i, c := range group.Checks {
				if got[i] == set.Answers[i] {
					continue
				}
				changed++
				if changed <= 20 {
					t.Errorf("%s: %s asked %s answers %c, snapshot says %c", group.Name, describeSnapshotGrants(committed, set.Grants), describeSnapshotCheck(committed.Checks[c]), got[i], set.Answers[i])
				}
			}
		}
	}
	if changed > 20 {
		t.Errorf("... %d answers changed in all", changed)
	}
}

const decisionSnapshotPath = "testdata/decision_snapshot.json"

var writeDecisionSnapshot = flag.Bool("write-decision-snapshot", false, "regenerate "+decisionSnapshotPath)

// sameSnapshotCases reports whether two snapshots ask the same questions of
// the same grants, whatever their answers.
func sameSnapshotCases(a, b snapshotFile) bool {
	if !reflect.DeepEqual(a.Grants, b.Grants) || !reflect.DeepEqual(a.Checks, b.Checks) || len(a.Groups) != len(b.Groups) {
		return false
	}
	for i := range a.Groups {
		ga, gb := a.Groups[i], b.Groups[i]
		if ga.Name != gb.Name || !slices.Equal(ga.Checks, gb.Checks) || len(ga.Sets) != len(gb.Sets) {
			return false
		}
		for j := range ga.Sets {
			if !slices.Equal(ga.Sets[j].Grants, gb.Sets[j].Grants) || len(ga.Sets[j].Answers) != len(ga.Checks) {
				return false
			}
		}
	}
	return true
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
			return fmt.Errorf("encode snapshot line: %w", err)
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
			return nil, fmt.Errorf("encode snapshot group %q: %w", group.Name, err)
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

// The two helpers below are the parts of the decision snapshot that depend on
// this branch's API. To compare with another version of the evaluator, copy
// decision_snapshot_cases_test.go there and rewrite these two for its API.

func snapshotMCPServer(id, project string) MCPServerRef {
	return MCPServer(MCPServerIDFrom(id), ProjectIDFrom(project))
}

func snapshotRawCheck(scope Scope, kind, id string, dimensions map[string]string) Check {
	return Check{Scope: scope, ResourceKind: kind, ResourceID: id, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}
