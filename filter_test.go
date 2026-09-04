package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFilterVSphere(t *testing.T) {
	jobs := []string{
		"periodic-ci-openshift-eng-agent-qe-infra-release-4.22-amd64-nightly-vsphere-agent-ha-f28", // keep: agent
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-vsphere-ipi-f28", // remove
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-vsphere-upi-zones-f7-longrun-mco-tp-p2", // keep: mco
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-vsphere-ipi-windows-f28", // keep: windows
		"pull-ci-openshift-priv-machine-config-operator-release-4.22-e2e-vsphere", // keep: machine config
		"pull-ci-openshift-priv-openshift-windows-machine-config-operator-release-4.22-unit", // wmco+windows+machine config (no vsphere in name -> dropped entirely)
		"rehearse-77070-periodic-ci-openshift-openshift-tests-private-release-5.0-amd64-nightly-vsphere-ipi-multi-vcenter-f28", // remove
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-azure-ovn", // not vsphere -> dropped entirely
	}
	keep, remove := filterVSphere(jobs)
	wantKeep := map[string]bool{}
	for _, s := range keep {
		wantKeep[s] = true
	}
	if len(keep) != 4 {
		t.Fatalf("keep = %d, want 4: %v", len(keep), keep)
	}
	if len(remove) != 2 {
		t.Fatalf("remove = %d, want 2: %v", len(remove), remove)
	}
	for _, s := range keep {
		if !wantKeep[s] {
			t.Errorf("unexpected keep: %s", s)
		}
	}
}

func TestMatchesRemove(t *testing.T) {
	remove := map[string]bool{
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-vsphere-ipi-f28":                          true,
		"pull-ci-openshift-priv-cloud-provider-vsphere-release-4.22-e2e-vsphere":                                            true,
		"periodic-ci-openshift-openshift-tests-private-release-5.0-amd64-nightly-vsphere-ipi-f14-sanity-reliability-test": true,
	}
	cases := []struct {
		testName string
		want     bool
	}{
		{"vsphere-ipi-f28", true},              // suffix of periodic
		{"e2e-vsphere", true},                  // suffix of pull-ci
		{"vsphere-ipi-f28-destructive", false}, // keep-name not in remove set
		{"azure-ipi-f28", false},
		{"4.22-upgrade-from-stable-4.21-vsphere-ipi-ovn-dualstack-f28", false}, // only if absent from remove set
		{"test", false},                        // regression pin: must not match suffix on job ending in -test
		{"", false},                            // empty test name never matches
	}
	for _, c := range cases {
		if got := matchesRemove(c.testName, remove); got != c.want {
			t.Errorf("matchesRemove(%q) = %v, want %v", c.testName, got, c.want)
		}
	}
}

func TestFindMappingValue(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "val1"},
			{Kind: yaml.ScalarNode, Value: "key2"},
			{Kind: yaml.ScalarNode, Value: "val2"},
		},
	}
	if v := findMappingValue(node, "key1"); v == nil || v.Value != "val1" {
		t.Errorf("expected val1, got %v", v)
	}
	if v := findMappingValue(node, "key2"); v == nil || v.Value != "val2" {
		t.Errorf("expected val2, got %v", v)
	}
	if v := findMappingValue(node, "nonexistent"); v != nil {
		t.Errorf("expected nil for nonexistent key, got %v", v)
	}
}

func TestApplyRemovals(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(filepath.Join(srcDir, "org", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	testYaml := `base_images:
  base:
    name: "4.22"
    namespace: ocp
    tag: base
tests:
- as: vsphere-ipi-f28
  commands: test
  container:
    from: base
- as: keep-this-test
  commands: test
  container:
    from: base
`
	unchangedYaml := `base_images:
  base:
    name: "4.22"
tests:
- as: keep-this-test
  commands: test
`
	srcFile1 := filepath.Join(srcDir, "org", "repo", "test1.yaml")
	srcFile2 := filepath.Join(srcDir, "org", "repo", "test2.yaml")
	if err := os.WriteFile(srcFile1, []byte(testYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile2, []byte(unchangedYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	removeSet := map[string]bool{
		"periodic-ci-openshift-openshift-tests-private-release-4.22-amd64-nightly-vsphere-ipi-f28": true,
	}

	// 1. Test outputDir mirror
	if err := applyRemovals(srcDir, outDir, false, removeSet); err != nil {
		t.Fatalf("applyRemovals failed: %v", err)
	}

	destFile1 := filepath.Join(outDir, "org", "repo", "test1.yaml")
	destFile2 := filepath.Join(outDir, "org", "repo", "test2.yaml")

	if _, err := os.Stat(destFile2); !os.IsNotExist(err) {
		t.Errorf("expected test2.yaml not to be created in outDir since it was unchanged")
	}

	b, err := os.ReadFile(destFile1)
	if err != nil {
		t.Fatalf("reading destFile1: %v", err)
	}
	content := string(b)
	if strings.Contains(content, "vsphere-ipi-f28") {
		t.Errorf("expected vsphere-ipi-f28 to be removed, got:\n%s", content)
	}
	if !strings.Contains(content, "keep-this-test") {
		t.Errorf("expected keep-this-test to be kept, got:\n%s", content)
	}
	if !strings.Contains(content, "base_images:") {
		t.Errorf("expected base_images to be preserved, got:\n%s", content)
	}

	// 2. Test inPlace
	if err := applyRemovals(srcDir, "", true, removeSet); err != nil {
		t.Fatalf("inPlace applyRemovals failed: %v", err)
	}
	bInPlace, err := os.ReadFile(srcFile1)
	if err != nil {
		t.Fatalf("reading inPlace file: %v", err)
	}
	if strings.Contains(string(bInPlace), "vsphere-ipi-f28") {
		t.Errorf("expected vsphere-ipi-f28 removed in-place, got:\n%s", string(bInPlace))
	}
	if !strings.Contains(string(bInPlace), "keep-this-test") {
		t.Errorf("expected keep-this-test preserved in-place, got:\n%s", string(bInPlace))
	}
}


