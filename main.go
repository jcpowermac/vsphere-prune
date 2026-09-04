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
	if as == "" || !strings.Contains(as, "vsphere") {
		return false
	}
	for j := range removeSet {
		if j == as || strings.HasSuffix(j, "-"+as) {
			return true
		}
	}
	return false
}

// applyRemovals re-walks the config dir and removes test entries whose `as`
// name matches the remove set. Writes in-place or to a mirror tree under
// outputDir (relative paths preserved, like writer.go computeRelativePath).
func applyRemovals(configs, outputDir string, inPlace bool, removeSet map[string]bool) error {
	var nodeParseErrors []string
	err := filepath.Walk(configs, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			nodeParseErrors = append(nodeParseErrors, path)
			return nil // not yaml at all; dry-run walker already classified it
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			return nil
		}
		root := doc.Content[0]
		if root.Kind != yaml.MappingNode {
			return nil
		}
		testsNode := findMappingValue(root, "tests")
		if testsNode == nil || testsNode.Kind != yaml.SequenceNode {
			return nil
		}
		var kept []*yaml.Node
		changed := false
		for _, testNode := range testsNode.Content {
			as := ""
			if testNode.Kind == yaml.MappingNode {
				if v := findMappingValue(testNode, "as"); v != nil {
					as = v.Value
				}
			}
			if as != "" && matchesRemove(as, removeSet) {
				changed = true
				continue
			}
			kept = append(kept, testNode)
		}
		if !changed {
			return nil
		}
		testsNode.Content = kept
		rel, err := filepath.Rel(configs, path)
		if err != nil {
			return err
		}
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return fmt.Errorf("%s: marshal: %w", path, err)
		}
		dest := path
		if !inPlace {
			dest = filepath.Join(outputDir, rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(dest, out, 0o644); err != nil {
			return err
		}
		fmt.Printf("APPLIED %s (now %d tests)\n", rel, len(kept))
		return nil
	})
	if err != nil {
		return err
	}
	if len(nodeParseErrors) > 0 {
		fmt.Printf("NODE PARSE ERRORS (skipped files): %d\n", len(nodeParseErrors))
		for _, e := range nodeParseErrors {
			fmt.Printf("  %s\n", e)
		}
	} else {
		fmt.Printf("NODE PARSE ERRORS: 0\n")
	}
	return nil
}

// findMappingValue finds a value node for a given key in a mapping node.
func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
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
