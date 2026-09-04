package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"

	prowapi "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"
)

var keepRe = regexp.MustCompile(`(?i)agent|machine.?config|mco|wmco|windows`)

func loadProwJobNames(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list prowapi.ProwJobList
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	names := make([]string, 0, len(list.Items))
	for _, pj := range list.Items {
		if pj.Spec.Job != "" {
			names = append(names, pj.Spec.Job)
		}
	}
	return names, nil
}

// filterVSphere returns (keep, remove) among jobs whose name contains "vsphere".
// Names without "vsphere" are out of scope.
func filterVSphere(jobs []string) (keep, remove []string) {
	seen := map[string]bool{}
	for _, j := range jobs {
		if seen[j] || !regexp.MustCompile(`vsphere`).MatchString(j) {
			continue
		}
		seen[j] = true
		if keepRe.MatchString(j) {
			keep = append(keep, j)
		} else {
			remove = append(remove, j)
		}
	}
	sort.Strings(keep)
	sort.Strings(remove)
	return keep, remove
}

func main() {
	prowjobsPath := flag.String("prowjobs", "/home/jcallen/Development/release/prowjobs.json", "path to prowjobs.json export")
	flag.Parse()

	names, err := loadProwJobNames(*prowjobsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keep, remove := filterVSphere(names)
	fmt.Printf("total=%d keep=%d remove=%d\n", len(keep)+len(remove), len(keep), len(remove))
}
