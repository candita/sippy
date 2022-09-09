package api

import (
	"encoding/json"
	"net/http"
	"strings"

	apitype "github.com/openshift/sippy/pkg/apis/api"
	"github.com/openshift/sippy/pkg/db"
	"github.com/openshift/sippy/pkg/db/query"
	"github.com/openshift/sippy/pkg/testidentification"
	"github.com/openshift/sippy/pkg/util/sets"
	log "github.com/sirupsen/logrus"
)

// PrintInstallJSONReportFromDB renders a report showing the success/fail rates of operator installation.
func PrintInstallJSONReportFromDB(w http.ResponseWriter, dbc *db.DB, release string) {

	exactTestNames := sets.NewString(testidentification.InstallTestName)
	testPrefixes := sets.NewString(
		testidentification.OperatorInstallPrefix,
		testidentification.InstallTestNamePrefix,
	)

	variantColumns, tests, err := VariantTestsReport(dbc, release, exactTestNames, testPrefixes, sets.NewString())
	if err != nil {
		log.WithError(err).Error("could not generate install report")
		RespondWithJSON(http.StatusInternalServerError, w, map[string]interface{}{"code": http.StatusInternalServerError, "message": "Could not generate install report: " + err.Error()})
		return
	}

	// Build up a set of column names, every variant we encounter as well as an "All":
	summary := map[string]interface{}{
		"title":        "Install Rates by Operator",
		"description":  "Install Rates by Operator by Variant",
		"column_names": variantColumns.List(),
		"tests":        tests,
	}

	result, err := json.Marshal(summary)
	if err != nil {
		log.WithError(err).Error("could not generate install report")
		RespondWithJSON(http.StatusInternalServerError, w, map[string]interface{}{"code": http.StatusInternalServerError, "message": "Could not generate install report: " + err.Error()})
		return
	}

	jsonStr := string(result)
	RespondWithJSON(http.StatusOK, w, jsonStr)
}

// VariantTestsReport returns a set of all variant columns plus "All", and a map of testName to variant column to test results for that variant.
// Caller can provide exact test names to match, test name prefixes, or test substrings.
func VariantTestsReport(dbc *db.DB, release string, testNames, testPrefixes, testSubStrings sets.String) (sets.String, map[string]map[string]apitype.Test, error) {

	// Build a list of all sub-strings to search for, we'll sort out exact matches later as these
	// can pickup unintented tests.
	testSearchStrings := sets.NewString(testNames.List()...)
	testSearchStrings.Insert(testPrefixes.List()...)
	testSearchStrings.Insert(testSubStrings.List()...)

	testReports, err := query.TestReportsByVariant(dbc, release, testSearchStrings.List())
	if err != nil {
		return sets.NewString(), map[string]map[string]apitype.Test{}, err
	}

	variantColumns := sets.NewString()
	variantColumns.Insert("All") // Insert the default overall "All" column we also display with the variants.
	tests := make(map[string]map[string]apitype.Test)

	for _, tr := range testReports {
		var prefixMatches bool
		var subStringMatches bool
		for _, prefix := range testPrefixes.List() {
			if strings.HasPrefix(tr.Name, prefix) {
				prefixMatches = true
				break
			}
		}
		for _, subString := range testSubStrings.List() {
			if strings.HasPrefix(tr.Name, subString) {
				subStringMatches = true
				break
			}
		}

		switch {
		case testNames.Has(tr.Name) || prefixMatches || subStringMatches:
			log.Infof("Found test %s for variant %s", tr.Name, tr.Variant)
			variantColumns.Insert(tr.Variant)
			if _, ok := tests[tr.Name]; !ok {
				tests[tr.Name] = map[string]apitype.Test{}
			}
			tests[tr.Name][tr.Variant] = tr
		default:
			// Our substring searching can pickup unintended tests:
			log.Infof("Ignoring test %s for variant %s", tr.Name, tr.Variant)
		}
	}

	// Add in the All column for each test:
	for testName := range tests {
		allReport, err := query.TestReportExcludeVariants(dbc, release, testName, []string{})
		if err != nil {
			return variantColumns, tests, err
		}
		tests[testName]["All"] = allReport
	}

	return variantColumns, tests, nil
}
