package log

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/ekristen/libnuke/pkg/resource"
)

var (
	ReasonSkip            = *color.New(color.FgYellow)
	ReasonError           = *color.New(color.FgRed)
	ReasonRemoveTriggered = *color.New(color.FgGreen)
	ReasonWaitPending     = *color.New(color.FgBlue)
	ReasonWaitDependency  = *color.New(color.FgCyan)
	ReasonSuccess         = *color.New(color.FgGreen)
)

var (
	ColorRegion             = *color.New(color.Bold)
	ColorResourceType       = *color.New()
	ColorResourceID         = *color.New(color.Bold)
	ColorResourceProperties = *color.New(color.Italic)
)

// Sorted -- Format the resource properties in sorted order ready for printing.
// This ensures that multiple runs of aws-nuke produce stable output so
// that they can be compared with each other.
func Sorted(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make([]string, 0, len(m))
	for k := range keys {
		sorted = append(sorted, fmt.Sprintf("%s: \"%s\"", keys[k], m[keys[k]]))
	}
	return fmt.Sprintf("[%s]", strings.Join(sorted, ", "))
}

func Log(scope string, resourceType string, r resource.Resource, c color.Color, msg string) {
	ColorRegion.Printf("%s", scope)
	fmt.Printf(" - ")
	ColorResourceType.Print(resourceType)
	fmt.Printf(" - ")

	rString, ok := r.(resource.LegacyStringer)
	if ok {
		ColorResourceID.Print(rString.String())
		fmt.Printf(" - ")
	}

	rProp, ok := r.(resource.PropertyGetter)
	if ok {
		ColorResourceProperties.Print(Sorted(rProp.Properties()))
		fmt.Printf(" - ")
	}

	c.Printf("%s\n", msg)
}
