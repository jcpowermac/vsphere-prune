package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	cioperatorapi "github.com/openshift/ci-tools/pkg/api"
	"gopkg.in/yaml.v3"
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

func testNames(cfg cioperatorapi.ReleaseBuildConfiguration) []string {
	out := make([]string, 0, len(cfg.Tests))
	for _, t := range cfg.Tests {
		out = append(out, t.As)
	}
	return out
}

// matchesRemove reports whether a test's `as` name is a job-name suffix of any
// REMOVE job. Test names are long and specific; suffix match on "-<as>" is
// precise enough (verified by the coverage report in Task 3 Step 2).
func matchesRemove(as string, removeSet map[string]bool) bool {
	for j := range removeSet {
		if j == as || strings.HasSuffix(j, "-"+as) {
			return true
		}
	}
	return false
}

func applyRemovals(configs, outputDir string, inPlace bool, removeSet map[string]bool) error {
	return nil
}

func main() {
	configs := flag.String("config-dir", "/home/jcallen/Development/release/ci-operator/config", "ci-operator/config dir")
	prowjobs := flag.String("prowjobs", "/home/jcallen/Development/release/prowjobs.json", "private deck prowjobs.json")
	outputDir := flag.String("output-dir", "", "mirror tree to write modified files into")
	inPlace := flag.Bool("in-place", false, "write changes back to the original config files")
	dryRun := flag.Bool("dry-run", true, "report only, do not write modified files")
	flag.Parse()
	if *inPlace && *outputDir != "" {
		log.Fatal("--in-place and --output-dir are mutually exclusive")
	}
	apply := *inPlace || (*outputDir != "" && !*dryRun)
	var parseErrors []string // collected by the walker below

	names, err := loadProwJobNames(*prowjobs)
	if err != nil {
		log.Fatal(err)
	}
	keep, remove := filterVSphere(names)
	removeSet := map[string]bool{}
	for _, j := range remove {
		removeSet[j] = true
	}

	matched := map[string]bool{} // remove job names matched to a config test
	keepHits := 0
	err = filepath.WalkDir(*configs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var cfg cioperatorapi.ReleaseBuildConfiguration
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			parseErrors = append(parseErrors, path) // like fix-vsphere-jobs: collect, don't abort
			return nil
		}
		if len(cfg.Tests) == 0 {
			return nil
		}
		for _, as := range testNames(cfg) {
			if !strings.Contains(as, "vsphere") {
				continue
			}
			if matchesRemove(as, removeSet) {
				rel, _ := filepath.Rel(*configs, path)
				fmt.Printf("REMOVE %s  test=%s\n", rel, as)
				for j := range removeSet {
					if j == as || strings.HasSuffix(j, "-"+as) {
						matched[j] = true
					}
				}
			} else {
				rel, _ := filepath.Rel(*configs, path)
				fmt.Printf("keep   %s  test=%s\n", rel, as)
				keepHits++
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	var unmatched []string
	for j := range removeSet {
		if !matched[j] {
			unmatched = append(unmatched, j)
		}
	}
	sort.Strings(unmatched)
	fmt.Printf("\nstats: remove=%d keep=%d | tests-removed=%d tests-kept=%d | unmatched-remove-jobs=%d | parse-errors=%d\n",
		len(removeSet), len(keep), len(matched), keepHits, len(unmatched), len(parseErrors))
	for _, j := range unmatched {
		fmt.Printf("UNMATCHED %s\n", j)
	}
	if len(parseErrors) > 0 {
		fmt.Printf("PARSE ERRORS (skipped files):\n")
		for _, e := range parseErrors {
			fmt.Printf("  %s\n", e)
		}
	}
	if !apply {
		return
	}
	if err := applyRemovals(*configs, *outputDir, *inPlace, removeSet); err != nil {
		log.Fatal(err) // Task 3
	}
}
