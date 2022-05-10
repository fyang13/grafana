package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana/pkg/infra/tracing"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	acdb "github.com/grafana/grafana/pkg/services/accesscontrol/database"
	"github.com/grafana/grafana/pkg/services/accesscontrol/resourcepermissions/types"
	apimodels "github.com/grafana/grafana/pkg/services/ngalert/api/tooling/definitions"
	ngmodels "github.com/grafana/grafana/pkg/services/ngalert/models"
	"github.com/grafana/grafana/pkg/tests/testinfra"
)

func TestPrometheusRules(t *testing.T) {
	_, err := tracing.InitializeTracerForTest()
	require.NoError(t, err)

	dir, path := testinfra.CreateGrafDir(t, testinfra.GrafanaOpts{
		DisableLegacyAlerting: true,
		EnableUnifiedAlerting: true,
		DisableAnonymous:      true,
		AppModeProduction:     true,
	})

	grafanaListedAddr, store := testinfra.StartGrafana(t, dir, path)

	// Create a user to make authenticated requests
	createUser(t, store, models.CreateUserCommand{
		DefaultOrgRole: string(models.ROLE_EDITOR),
		Password:       "password",
		Login:          "grafana",
	})

	// Create the namespace we'll save our alerts to.
	err = createFolder(t, "default", grafanaListedAddr, "grafana", "password")
	require.NoError(t, err)

	interval, err := model.ParseDuration("10s")
	require.NoError(t, err)

	// an unauthenticated request to get rules should fail
	{
		promRulesURL := fmt.Sprintf("http://%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		require.NoError(t, err)
		assert.Equal(t, 401, resp.StatusCode)
	}

	// When we have no alerting rules, it returns an empty list.
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		require.JSONEq(t, `{"status": "success", "data": {"groups": []}}`, string(b))
	}

	// Now, let's create some rules
	{
		rules := apimodels.PostableRuleGroupConfig{
			Name: "arulegroup",
			Rules: []apimodels.PostableExtendedRuleNode{
				{
					ApiRuleNode: &apimodels.ApiRuleNode{
						For:         interval,
						Labels:      map[string]string{"label1": "val1"},
						Annotations: map[string]string{"annotation1": "val1"},
					},
					// this rule does not explicitly set no data and error states
					// therefore it should get the default values
					GrafanaManagedAlert: &apimodels.PostableGrafanaRule{
						Title:     "AlwaysFiring",
						Condition: "A",
						Data: []ngmodels.AlertQuery{
							{
								RefID: "A",
								RelativeTimeRange: ngmodels.RelativeTimeRange{
									From: ngmodels.Duration(time.Duration(5) * time.Hour),
									To:   ngmodels.Duration(time.Duration(3) * time.Hour),
								},
								DatasourceUID: "-100",
								Model: json.RawMessage(`{
									"type": "math",
									"expression": "2 + 3 > 1"
									}`),
							},
						},
					},
				},
				{
					GrafanaManagedAlert: &apimodels.PostableGrafanaRule{
						Title:     "AlwaysFiringButSilenced",
						Condition: "A",
						Data: []ngmodels.AlertQuery{
							{
								RefID: "A",
								RelativeTimeRange: ngmodels.RelativeTimeRange{
									From: ngmodels.Duration(time.Duration(5) * time.Hour),
									To:   ngmodels.Duration(time.Duration(3) * time.Hour),
								},
								DatasourceUID: "-100",
								Model: json.RawMessage(`{
									"type": "math",
									"expression": "2 + 3 > 1"
									}`),
							},
						},
						NoDataState:  apimodels.NoDataState(ngmodels.Alerting),
						ExecErrState: apimodels.ExecutionErrorState(ngmodels.AlertingErrState),
					},
				},
			},
		}
		buf := bytes.Buffer{}
		enc := json.NewEncoder(&buf)
		err := enc.Encode(&rules)
		require.NoError(t, err)

		u := fmt.Sprintf("http://grafana:password@%s/api/ruler/grafana/api/v1/rules/default", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Post(u, "application/json", &buf)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, resp.StatusCode, 202)
		require.JSONEq(t, `{"message":"rule group updated successfully"}`, string(b))
	}

	// Check that we cannot create a rule that has a panel_id and no dashboard_uid
	{
		rules := apimodels.PostableRuleGroupConfig{
			Name: "anotherrulegroup",
			Rules: []apimodels.PostableExtendedRuleNode{
				{
					ApiRuleNode: &apimodels.ApiRuleNode{
						For:         interval,
						Labels:      map[string]string{},
						Annotations: map[string]string{"__panelId__": "1"},
					},
					// this rule does not explicitly set no data and error states
					// therefore it should get the default values
					GrafanaManagedAlert: &apimodels.PostableGrafanaRule{
						Title:     "NeverCreated",
						Condition: "A",
						Data: []ngmodels.AlertQuery{
							{
								RefID: "A",
								RelativeTimeRange: ngmodels.RelativeTimeRange{
									From: ngmodels.Duration(time.Duration(5) * time.Hour),
									To:   ngmodels.Duration(time.Duration(3) * time.Hour),
								},
								DatasourceUID: "-100",
								Model: json.RawMessage(`{
									"type": "math",
									"expression": "2 + 3 > 1"
									}`),
							},
						},
					},
				},
			},
		}
		buf := bytes.Buffer{}
		enc := json.NewEncoder(&buf)
		err := enc.Encode(&rules)
		require.NoError(t, err)

		u := fmt.Sprintf("http://grafana:password@%s/api/ruler/grafana/api/v1/rules/default", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Post(u, "application/json", &buf)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, 400, resp.StatusCode)
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(b, &res))
		require.Equal(t, "invalid rule specification at index [0]: both annotations __dashboardUid__ and __panelId__ must be specified", res["message"])
	}

	// Now, let's see how this looks like.
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, `
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "arulegroup",
			"file": "default",
			"rules": [{
				"state": "inactive",
				"name": "AlwaysFiring",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 10,
				"annotations": {
					"annotation1": "val1"
				},
				"labels": {
					"label1": "val1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}, {
				"state": "inactive",
				"name": "AlwaysFiringButSilenced",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, string(b))
	}

	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		require.Eventually(t, func() bool {
			resp, err := http.Get(promRulesURL)
			require.NoError(t, err)
			t.Cleanup(func() {
				err := resp.Body.Close()
				require.NoError(t, err)
			})
			b, err := ioutil.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			require.JSONEq(t, `
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "arulegroup",
			"file": "default",
			"rules": [{
				"state": "inactive",
				"name": "AlwaysFiring",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 10,
				"annotations": {
					"annotation1": "val1"
				},
				"labels": {
					"label1": "val1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}, {
				"state": "inactive",
				"name": "AlwaysFiringButSilenced",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, string(b))
			return true
		}, 18*time.Second, 2*time.Second)
	}
}

func TestPrometheusRulesFilterByDashboard(t *testing.T) {
	_, err := tracing.InitializeTracerForTest()
	require.NoError(t, err)
	dir, path := testinfra.CreateGrafDir(t, testinfra.GrafanaOpts{
		EnableFeatureToggles: []string{"ngalert"},
		DisableAnonymous:     true,
		AppModeProduction:    true,
	})

	grafanaListedAddr, store := testinfra.StartGrafana(t, dir, path)

	// Create a user to make authenticated requests
	createUser(t, store, models.CreateUserCommand{
		DefaultOrgRole: string(models.ROLE_EDITOR),
		Password:       "password",
		Login:          "grafana",
	})

	// Create the namespace we'll save our alerts to.
	dashboardUID := "default"
	err = createFolder(t, dashboardUID, grafanaListedAddr, "grafana", "password")
	require.NoError(t, err)

	interval, err := model.ParseDuration("10s")
	require.NoError(t, err)

	// Now, let's create some rules
	{
		rules := apimodels.PostableRuleGroupConfig{
			Name: "anotherrulegroup",
			Rules: []apimodels.PostableExtendedRuleNode{
				{
					ApiRuleNode: &apimodels.ApiRuleNode{
						For:    interval,
						Labels: map[string]string{},
						Annotations: map[string]string{
							"__dashboardUid__": dashboardUID,
							"__panelId__":      "1",
						},
					},
					GrafanaManagedAlert: &apimodels.PostableGrafanaRule{
						Title:     "AlwaysFiring",
						Condition: "A",
						Data: []ngmodels.AlertQuery{
							{
								RefID: "A",
								RelativeTimeRange: ngmodels.RelativeTimeRange{
									From: ngmodels.Duration(time.Duration(5) * time.Hour),
									To:   ngmodels.Duration(time.Duration(3) * time.Hour),
								},
								DatasourceUID: "-100",
								Model: json.RawMessage(`{
									"type": "math",
									"expression": "2 + 3 > 1"
									}`),
							},
						},
					},
				},
				{
					GrafanaManagedAlert: &apimodels.PostableGrafanaRule{
						Title:     "AlwaysFiringButSilenced",
						Condition: "A",
						Data: []ngmodels.AlertQuery{
							{
								RefID: "A",
								RelativeTimeRange: ngmodels.RelativeTimeRange{
									From: ngmodels.Duration(time.Duration(5) * time.Hour),
									To:   ngmodels.Duration(time.Duration(3) * time.Hour),
								},
								DatasourceUID: "-100",
								Model: json.RawMessage(`{
									"type": "math",
									"expression": "2 + 3 > 1"
									}`),
							},
						},
						NoDataState:  apimodels.NoDataState(ngmodels.Alerting),
						ExecErrState: apimodels.ExecutionErrorState(ngmodels.AlertingErrState),
					},
				},
			},
		}
		buf := bytes.Buffer{}
		enc := json.NewEncoder(&buf)
		err := enc.Encode(&rules)
		require.NoError(t, err)

		u := fmt.Sprintf("http://grafana:password@%s/api/ruler/grafana/api/v1/rules/default", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Post(u, "application/json", &buf)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, resp.StatusCode, 202)
		require.JSONEq(t, `{"message":"rule group updated successfully"}`, string(b))
	}

	expectedAllJSON := fmt.Sprintf(`
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "anotherrulegroup",
			"file": "default",
			"rules": [{
				"state": "inactive",
				"name": "AlwaysFiring",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 10,
				"annotations": {
					"__dashboardUid__": "%s",
					"__panelId__": "1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}, {
				"state": "inactive",
				"name": "AlwaysFiringButSilenced",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, dashboardUID)
	expectedFilteredByJSON := fmt.Sprintf(`
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "anotherrulegroup",
			"file": "default",
			"rules": [{
				"state": "inactive",
				"name": "AlwaysFiring",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 10,
				"annotations": {
					"__dashboardUid__": "%s",
					"__panelId__": "1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, dashboardUID)
	expectedNoneJSON := `
{
	"status": "success",
	"data": {
		"groups": []
	}
}`

	// Now, let's see how this looks like.
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, expectedAllJSON, string(b))
	}

	// Now, let's check we get the same rule when filtering by dashboard_uid
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?dashboard_uid=%s", grafanaListedAddr, dashboardUID)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, expectedFilteredByJSON, string(b))
	}

	// Now, let's check we get no rules when filtering by an unknown dashboard_uid
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?dashboard_uid=%s", grafanaListedAddr, "abc")
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, expectedNoneJSON, string(b))
	}

	// Now, let's check we get the same rule when filtering by dashboard_uid and panel_id
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?dashboard_uid=%s&panel_id=1", grafanaListedAddr, dashboardUID)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, expectedFilteredByJSON, string(b))
	}

	// Now, let's check we get no rules when filtering by dashboard_uid and unknown panel_id
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?dashboard_uid=%s&panel_id=2", grafanaListedAddr, dashboardUID)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, expectedNoneJSON, string(b))
	}

	// Now, let's check an invalid panel_id returns a 400 Bad Request response
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?dashboard_uid=%s&panel_id=invalid", grafanaListedAddr, dashboardUID)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(b, &res))
		require.Equal(t, `invalid panel_id: strconv.ParseInt: parsing "invalid": invalid syntax`, res["message"])
	}

	// Now, let's check a panel_id without dashboard_uid returns a 400 Bad Request response
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules?panel_id=1", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(b, &res))
		require.Equal(t, "panel_id must be set with dashboard_uid", res["message"])
	}
}

func TestPrometheusRulesPermissions(t *testing.T) {
	_, err := tracing.InitializeTracerForTest()
	require.NoError(t, err)

	dir, path := testinfra.CreateGrafDir(t, testinfra.GrafanaOpts{
		DisableLegacyAlerting: true,
		EnableUnifiedAlerting: true,
		DisableAnonymous:      true,
		AppModeProduction:     true,
	})

	grafanaListedAddr, store := testinfra.StartGrafana(t, dir, path)

	// Create a user to make authenticated requests
	userID := createUser(t, store, models.CreateUserCommand{
		DefaultOrgRole: string(models.ROLE_EDITOR),
		Password:       "password",
		Login:          "grafana",
	})

	// access control permissions store
	permissionsStore := acdb.ProvideService(store)

	// Create the namespace we'll save our alerts to.
	err = createFolder(t, "folder1", grafanaListedAddr, "grafana", "password")
	require.NoError(t, err)

	// Create the namespace we'll save our alerts to.
	err = createFolder(t, "folder2", grafanaListedAddr, "grafana", "password")
	require.NoError(t, err)

	// Create rule under folder1
	createRule(t, grafanaListedAddr, "folder1", "grafana", "password")

	// Create rule under folder2
	createRule(t, grafanaListedAddr, "folder2", "grafana", "password")

	// Now, let's see how this looks like.
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, `
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "arulegroup",
			"file": "folder1",
			"rules": [{
				"state": "inactive",
				"name": "rule under folder folder1",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 120,
				"annotations": {
					"annotation1": "val1"
				},
				"labels": {
					"label1": "val1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		},
		{
			"name": "arulegroup",
			"file": "folder2",
			"rules": [{
				"state": "inactive",
				"name": "rule under folder folder2",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 120,
				"annotations": {
					"annotation1": "val1"
				},
				"labels": {
					"label1": "val1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, string(b))
	}

	// remove permissions from folder2
	removeFolderPermission(t, permissionsStore, 1, userID, models.ROLE_EDITOR, "folder2")
	reloadCachedPermissions(t, grafanaListedAddr, "grafana", "password")

	// make sure that folder2 is not included in the response
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, `
{
	"status": "success",
	"data": {
		"groups": [{
			"name": "arulegroup",
			"file": "folder1",
			"rules": [{
				"state": "inactive",
				"name": "rule under folder folder1",
				"query": "[{\"refId\":\"A\",\"queryType\":\"\",\"relativeTimeRange\":{\"from\":18000,\"to\":10800},\"datasourceUid\":\"-100\",\"model\":{\"expression\":\"2 + 3 \\u003e 1\",\"intervalMs\":1000,\"maxDataPoints\":43200,\"type\":\"math\"}}]",
				"duration": 120,
				"annotations": {
					"annotation1": "val1"
				},
				"labels": {
					"label1": "val1"
				},
				"health": "ok",
				"type": "alerting",
				"lastEvaluation": "0001-01-01T00:00:00Z",
				"evaluationTime": 0
			}],
			"interval": 60,
			"lastEvaluation": "0001-01-01T00:00:00Z",
			"evaluationTime": 0
		}]
	}
}`, string(b))
	}

	// remove permissions from folder1
	removeFolderPermission(t, permissionsStore, 1, userID, models.ROLE_EDITOR, "folder1")
	reloadCachedPermissions(t, grafanaListedAddr, "grafana", "password")

	// make sure that no folders are included in the response
	{
		promRulesURL := fmt.Sprintf("http://grafana:password@%s/api/prometheus/grafana/api/v1/rules", grafanaListedAddr)
		// nolint:gosec
		resp, err := http.Get(promRulesURL)
		require.NoError(t, err)
		t.Cleanup(func() {
			err := resp.Body.Close()
			require.NoError(t, err)
		})
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		require.JSONEq(t, `
{
	"status": "success",
	"data": {
		"groups": []
	}
}`, string(b))
	}
}

func reloadCachedPermissions(t *testing.T, addr, login, password string) {
	t.Helper()

	u := fmt.Sprintf("http://%s:%s@%s/api/access-control/user/permissions", login, password, addr)
	// nolint:gosec
	resp, err := http.Get(u)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func removeFolderPermission(t *testing.T, store *acdb.AccessControlStore, orgID, userID int64, role models.RoleType, uid string) {
	t.Helper()
	// remove user permissions on folder
	_, _ = store.SetUserResourcePermission(context.Background(), orgID, accesscontrol.User{ID: userID}, types.SetResourcePermissionCommand{
		Resource:          "folders",
		ResourceID:        uid,
		ResourceAttribute: "uid",
	}, nil)

	// remove org role permissions from folder
	_, _ = store.SetBuiltInResourcePermission(context.Background(), orgID, string(role), types.SetResourcePermissionCommand{
		Resource:          "folders",
		ResourceID:        uid,
		ResourceAttribute: "uid",
	}, nil)

	// remove org role children permissions from folder
	for _, c := range role.Children() {
		_, _ = store.SetBuiltInResourcePermission(context.Background(), orgID, string(c), types.SetResourcePermissionCommand{
			Resource:          "folders",
			ResourceID:        uid,
			ResourceAttribute: "uid",
		}, nil)
	}
}
