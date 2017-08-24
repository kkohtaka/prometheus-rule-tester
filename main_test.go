// Copyright (C) 2017 Kazumasa Kohtaka <kkohtaka@gmail.com> All right reserved
// This file is available under the MIT license.

package main_test

import (
	"io/ioutil"
	"testing"

	"github.com/prometheus/common/log"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
)

func TestAlertRules(t *testing.T) {
	type testCase struct {
		DataFile  string
		RulesFile string

		// TODO: Add parameters to check an expected behaviour
	}

	tcs := []testCase{
		testCase{
			"./testdata/case_001.dat",
			"./testdata/case_001.rules",
		},
	}
	for _, tc := range tcs {
		content, err := ioutil.ReadFile(tc.DataFile)
		if err != nil {
			t.Fatal(err)
		}
		suite, err := promql.NewTest(t, string(content))
		for err != nil {
			t.Fatal(err)
		}
		defer suite.Close()

		if err := suite.Run(); err != nil {
			t.Fatal(err)
		}

		alertRules := []rules.Rule{}
		content, err = ioutil.ReadFile(tc.RulesFile)
		if err != nil {
			t.Fatal(err)
		}
		statements, err := promql.ParseStmts(string(content))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			var rule rules.Rule
			switch r := statement.(type) {
			case *promql.AlertStmt:
				rule = rules.NewAlertingRule(
					r.Name,
					r.Expr,
					r.Duration,
					r.Labels,
					r.Annotations,
					log.Base())
			default:
				t.Fatal("Invalid statement type")
			}
			alertRules = append(alertRules, rule)
		}

		// TODO: Check an expected behaviour
		// for _, alertRule := range alertRules {
		// 	evalTime := time.Unix(0, 0)
		// 	samples, err := alertRule.Eval(suite.Context(), evalTime, suite.QueryEngine(), nil)
		// 	if err != nil {
		// 		t.Fatal(err)
		// 	}
		//
		// 	fmt.Println(len(samples))
		// 	for _, sample := range samples {
		// 		fmt.Println(sample.Metric)
		// 		fmt.Println(sample.Point.T, sample.Point.V)
		// 	}
		// }
	}
}
