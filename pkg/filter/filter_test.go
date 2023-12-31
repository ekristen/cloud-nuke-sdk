package filter_test

import (
	"github.com/ekristen/libnuke/pkg/filter"
	"strconv"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"
)

func TestUnmarshalFilter(t *testing.T) {
	past := time.Now().UTC().Add(-24 * time.Hour)
	future := time.Now().UTC().Add(24 * time.Hour)
	cases := []struct {
		yaml            string
		match, mismatch []string
	}{
		{
			yaml:     `foo`,
			match:    []string{"foo"},
			mismatch: []string{"fo", "fooo", "o", "fo"},
		},
		{
			yaml:     `{"type":"exact","value":"foo"}`,
			match:    []string{"foo"},
			mismatch: []string{"fo", "fooo", "o", "fo"},
		},
		{
			yaml:     `{"type":"glob","value":"b*sh"}`,
			match:    []string{"bish", "bash", "bosh", "bush", "boooooosh", "bsh"},
			mismatch: []string{"woooosh", "fooo", "o", "fo"},
		},
		{
			yaml:     `{"type":"glob","value":"b?sh"}`,
			match:    []string{"bish", "bash", "bosh", "bush"},
			mismatch: []string{"woooosh", "fooo", "o", "fo", "boooooosh", "bsh"},
		},
		{
			yaml:     `{"type":"regex","value":"b[iao]sh"}`,
			match:    []string{"bish", "bash", "bosh"},
			mismatch: []string{"woooosh", "fooo", "o", "fo", "boooooosh", "bsh", "bush"},
		},
		{
			yaml:     `{"type":"contains","value":"mba"}`,
			match:    []string{"bimbaz", "mba", "bi mba z"},
			mismatch: []string{"bim-baz"},
		},
		{
			yaml: `{"type":"dateOlderThan","value":"0"}`,
			match: []string{strconv.Itoa(int(future.Unix())),
				future.Format("2006-01-02"),
				future.Format("2006/01/02"),
				future.Format("2006-01-02T15:04:05Z"),
				future.Format(time.RFC3339Nano),
				future.Format(time.RFC3339),
			},
			mismatch: []string{"",
				strconv.Itoa(int(past.Unix())),
				past.Format("2006-01-02"),
				past.Format("2006/01/02"),
				past.Format("2006-01-02T15:04:05Z"),
				past.Format(time.RFC3339Nano),
				past.Format(time.RFC3339),
			},
		},
		{
			yaml:     `{"type":"prefix","value":"someprefix-"}`,
			match:    []string{"someprefix-1234", "someprefix-someprefix", "someprefix-asdafd"},
			mismatch: []string{"not-someprefix-1234", "not-someprefix-asfda"},
		},
		{
			yaml:     `{"type":"suffix","value":"-somesuffix"}`,
			match:    []string{"12345-somesuffix", "someprefix-somesuffix", "asdfdsa-somesuffix"},
			mismatch: []string{"1235-somesuffix-not", "asdf-not-somesuffix-not"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			var f filter.Filter

			err := yaml.Unmarshal([]byte(tc.yaml), &f)
			if err != nil {
				t.Fatal(err)
			}

			for _, o := range tc.match {
				match, err := f.Match(o)
				if err != nil {
					t.Fatal(err)
				}

				if !match {
					t.Fatalf("'%v' should match", o)
				}
			}

			for _, o := range tc.mismatch {
				match, err := f.Match(o)
				if err != nil {
					t.Fatal(err)
				}

				if match {
					t.Fatalf("'%v' should not match", o)
				}
			}
		})
	}

}
