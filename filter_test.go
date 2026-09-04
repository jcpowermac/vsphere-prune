package main

import "testing"

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
