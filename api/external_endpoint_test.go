package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TwiN/gatus/v5/alerting"
	"github.com/TwiN/gatus/v5/alerting/alert"
	"github.com/TwiN/gatus/v5/alerting/provider/discord"
	"github.com/TwiN/gatus/v5/client"
	"github.com/TwiN/gatus/v5/config"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"github.com/TwiN/gatus/v5/config/maintenance"
	"github.com/TwiN/gatus/v5/storage/store"
	"github.com/TwiN/gatus/v5/storage/store/common/paging"
	"github.com/TwiN/gatus/v5/test"
)

func TestCreateExternalEndpointResult(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	cfg := &config.Config{
		Alerting: &alerting.Config{
			Discord: &discord.AlertProvider{},
		},
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:  "n",
				Group: "g",
				Token: "token",
				Alerts: []*alert.Alert{
					{
						Type:             alert.TypeDiscord,
						FailureThreshold: 2,
						SuccessThreshold: 2,
					},
				},
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	scenarios := []struct {
		Name                           string
		Path                           string
		Body                           string
		AuthorizationHeaderBearerToken string
		ExpectedCode                   int
	}{
		{
			Name:                           "no-token",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			AuthorizationHeaderBearerToken: "",
			ExpectedCode:                   401,
		},
		{
			Name:                           "bad-token",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			AuthorizationHeaderBearerToken: "Bearer bad-token",
			ExpectedCode:                   401,
		},
		{
			Name:                           "bad-key",
			Path:                           "/api/v1/endpoints/bad_key/external?success=true",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   404,
		},
		{
			Name:                           "bad-success-value",
			Path:                           "/api/v1/endpoints/g_n/external?success=invalid",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "bad-duration-value",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&duration=invalid",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "good-token-success-true",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "good-token-success-true-with-ignored-error-because-success-true",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&error=failed",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "good-duration-success-true",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&duration=10s",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "good-token-success-false",
			Path:                           "/api/v1/endpoints/g_n/external?success=false",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "good-token-success-false-again",
			Path:                           "/api/v1/endpoints/g_n/external?success=false",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "good-token-success-false-with-error",
			Path:                           "/api/v1/endpoints/g_n/external?success=false&error=failed",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   200,
		},
		{
			Name:                           "malformed-body",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           "{",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "body-is-not-an-object",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           "[]",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "null-condition-result",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[null]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "blank-condition",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"  ","success":true}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "condition-too-long",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"` + strings.Repeat("a", maximumConditionLengthPerPush+1) + `","success":true}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "too-many-condition-results",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[` + strings.Repeat(`{"condition":"[STATUS] == 200","success":true},`, maximumNumberOfConditionResultsPerPush) + `{"condition":"[STATUS] == 200","success":true}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "null-condition-result-detail",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"c","success":true,"details":[null]}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "blank-condition-result-detail-label",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"c","success":true,"details":[{"label":" ","value":"v"}]}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "condition-result-detail-label-too-long",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"c","success":true,"details":[{"label":"` + strings.Repeat("a", maximumDetailLabelLength+1) + `","value":"v"}]}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "condition-result-detail-value-too-long",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"c","success":true,"details":[{"label":"l","value":"` + strings.Repeat("a", maximumDetailValueLength+1) + `"}]}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "too-many-condition-result-details",
			Path:                           "/api/v1/endpoints/g_n/external?success=true",
			Body:                           `{"conditionResults":[{"condition":"c","success":true,"details":[` + strings.Repeat(`{"label":"l","value":"v"},`, maximumNumberOfDetailsPerConditionResult) + `{"label":"l","value":"v"}]}]}`,
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "hostname-too-long",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&hostname=" + strings.Repeat("a", maximumHostnameLengthPerPush+1),
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "logo-too-long",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&logo=https://example.org/" + strings.Repeat("a", maximumLogoLengthPerPush),
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "logo-not-http",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&logo=" + url.QueryEscape("data:image/png;base64,AAAA"),
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
		{
			Name:                           "logo-relative",
			Path:                           "/api/v1/endpoints/g_n/external?success=true&logo=/logo.png",
			AuthorizationHeaderBearerToken: "Bearer token",
			ExpectedCode:                   400,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			var body io.Reader = http.NoBody
			if len(scenario.Body) > 0 {
				body = strings.NewReader(scenario.Body)
			}
			request := httptest.NewRequest("POST", scenario.Path, body)
			if len(scenario.AuthorizationHeaderBearerToken) > 0 {
				request.Header.Set("Authorization", scenario.AuthorizationHeaderBearerToken)
			}
			response, err := router.Test(request)
			if err != nil {
				return
			}
			defer response.Body.Close()
			if response.StatusCode != scenario.ExpectedCode {
				t.Errorf("%s %s should have returned %d, but returned %d instead", request.Method, request.URL, scenario.ExpectedCode, response.StatusCode)
			}
		})
	}
	t.Run("verify-end-results", func(t *testing.T) {
		endpointStatus, err := store.Get().GetEndpointStatus("g", "n", paging.NewEndpointStatusParams().WithResults(1, 11))
		if err != nil {
			t.Errorf("failed to get endpoint status: %s", err.Error())
			return
		}
		if endpointStatus.Key != "g_n" {
			t.Errorf("expected key to be g_n but got %s", endpointStatus.Key)
		}
		if len(endpointStatus.Results) != 6 {
			t.Errorf("expected 6 results but got %d", len(endpointStatus.Results))
		}
		if !endpointStatus.Results[0].Success {
			t.Errorf("expected first result to be successful")
		}
		if !endpointStatus.Results[1].Success {
			t.Errorf("expected second result to be successful")
		}
		if len(endpointStatus.Results[1].Errors) > 0 {
			t.Errorf("expected second result to have no errors")
		}
		if endpointStatus.Results[2].Duration == 0 || endpointStatus.Results[2].Duration.Seconds() != 10 {
			t.Errorf("expected third result to have a duration of 10 seconds")
		}
		if endpointStatus.Results[3].Success {
			t.Errorf("expected fourth result to be unsuccessful")
		}
		if endpointStatus.Results[4].Success {
			t.Errorf("expected fifth result to be unsuccessful")
		}
		if endpointStatus.Results[5].Success {
			t.Errorf("expected sixth result to be unsuccessful")
		}
		if len(endpointStatus.Results[5].Errors) == 0 || endpointStatus.Results[5].Errors[0] != "failed" {
			t.Errorf("expected sixth result to have errors: failed")
		}
		externalEndpointFromConfig := cfg.GetExternalEndpointByKey("g_n")
		if externalEndpointFromConfig.NumberOfFailuresInARow != 3 {
			t.Errorf("expected 3 failures in a row but got %d", externalEndpointFromConfig.NumberOfFailuresInARow)
		}
		if externalEndpointFromConfig.NumberOfSuccessesInARow != 0 {
			t.Errorf("expected 0 successes in a row but got %d", externalEndpointFromConfig.NumberOfSuccessesInARow)
		}
	})
}

func TestCreateExternalEndpointResultUnderEndpointMaintenanceWindow(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	externalEndpoint := &endpoint.ExternalEndpoint{
		Name:  "n",
		Group: "g",
		Token: "token",
		Alerts: []*alert.Alert{
			{
				Type:             alert.TypeDiscord,
				FailureThreshold: 2,
				SuccessThreshold: 2,
			},
		},
		MaintenanceWindows: []*maintenance.Config{{
			Start:    time.Now().In(location).Add(-time.Minute).Format("15:04"),
			Duration: time.Hour,
			Timezone: "Europe/Berlin",
		}},
	}
	if err := externalEndpoint.ValidateAndSetDefaults(); err != nil {
		t.Fatal("did not expect an error, got", err)
	}
	cfg := &config.Config{
		Alerting: &alerting.Config{
			Discord: &discord.AlertProvider{},
		},
		ExternalEndpoints: []*endpoint.ExternalEndpoint{externalEndpoint},
		Maintenance:       &maintenance.Config{},
	}
	router := New(cfg).Router()
	request := httptest.NewRequest("POST", "/api/v1/endpoints/g_n/external?success=false", http.NoBody)
	request.Header.Set("Authorization", "Bearer token")
	response, err := router.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if externalEndpoint.NumberOfFailuresInARow != 0 {
		t.Errorf("expected alerting to be skipped because the endpoint is under maintenance, but NumberOfFailuresInARow is %d", externalEndpoint.NumberOfFailuresInARow)
	}
}

func TestCreateExternalEndpointResultWithConditionResults(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	cfg := &config.Config{
		Alerting: &alerting.Config{
			Discord: &discord.AlertProvider{},
		},
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:  "n",
				Group: "g",
				Token: "token",
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	push := func(t *testing.T, path, body string) {
		t.Helper()
		var requestBody io.Reader = http.NoBody
		if len(body) > 0 {
			requestBody = strings.NewReader(body)
		}
		request := httptest.NewRequest("POST", path, requestBody)
		request.Header.Set("Authorization", "Bearer token")
		response, err := router.Test(request)
		if err != nil {
			t.Fatalf("failed to push result: %s", err.Error())
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s %s should have returned 200, but returned %d instead", request.Method, request.URL, response.StatusCode)
		}
	}
	// Even though one of the conditions is successful, the result must remain unsuccessful,
	// because success is determined by the query parameter and not by the condition results.
	push(t, "/api/v1/endpoints/g_n/external?success=false", `{"conditionResults":[{"condition":"[STATUS] == 200","success":true},{"condition":"[RESPONSE_TIME] < 300","success":false}],"somethingElse":1}`)
	push(t, "/api/v1/endpoints/g_n/external?success=true", "")
	push(t, "/api/v1/endpoints/g_n/external?success=true", "{}")
	push(t, "/api/v1/endpoints/g_n/external?success=true", "null")
	endpointStatus, err := store.Get().GetEndpointStatusByKey("g_n", paging.NewEndpointStatusParams().WithResults(1, 10))
	if err != nil {
		t.Fatalf("failed to get endpoint status: %s", err.Error())
	}
	if len(endpointStatus.Results) != 4 {
		t.Fatalf("expected 4 results but got %d", len(endpointStatus.Results))
	}
	if endpointStatus.Results[0].Success {
		t.Error("expected first result to be unsuccessful, because success comes from the query parameter")
	}
	if len(endpointStatus.Results[0].ConditionResults) != 2 {
		t.Fatalf("expected first result to have 2 condition results but got %d", len(endpointStatus.Results[0].ConditionResults))
	}
	if endpointStatus.Results[0].ConditionResults[0].Condition != "[STATUS] == 200" || !endpointStatus.Results[0].ConditionResults[0].Success {
		t.Errorf("expected first condition result to be '[STATUS] == 200' and successful, but got '%s' and %v", endpointStatus.Results[0].ConditionResults[0].Condition, endpointStatus.Results[0].ConditionResults[0].Success)
	}
	if endpointStatus.Results[0].ConditionResults[1].Condition != "[RESPONSE_TIME] < 300" || endpointStatus.Results[0].ConditionResults[1].Success {
		t.Errorf("expected second condition result to be '[RESPONSE_TIME] < 300' and unsuccessful, but got '%s' and %v", endpointStatus.Results[0].ConditionResults[1].Condition, endpointStatus.Results[0].ConditionResults[1].Success)
	}
	if len(endpointStatus.Results[1].ConditionResults) != 0 {
		t.Errorf("expected second result to have no condition results, because no body was sent")
	}
	if len(endpointStatus.Results[2].ConditionResults) != 0 {
		t.Errorf("expected third result to have no condition results, because the body was empty")
	}
	if len(endpointStatus.Results[3].ConditionResults) != 0 {
		t.Errorf("expected fourth result to have no condition results, because the body was null")
	}
}

func TestCreateExternalEndpointResultWithConditionResultDetails(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	cfg := &config.Config{
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:  "n",
				Group: "g",
				Token: "token",
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	request := httptest.NewRequest("POST", "/api/v1/endpoints/g_n/external?success=false", strings.NewReader(`{"conditionResults":[{"condition":"stream: 0 of 12 valid","success":false,"details":[{"label":"Attempt","value":"WARP · HTTP 403"},{"label":"Attempt","value":"direct · HTTP 200"},{"label":"Reason","value":""}]},{"condition":"manifest: reachable","success":true}]}`))
	request.Header.Set("Authorization", "Bearer token")
	response, err := router.Test(request)
	if err != nil {
		t.Fatalf("failed to push result: %s", err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	endpointStatus, err := store.Get().GetEndpointStatusByKey("g_n", paging.NewEndpointStatusParams().WithResults(1, 10))
	if err != nil {
		t.Fatalf("failed to get endpoint status: %s", err.Error())
	}
	if len(endpointStatus.Results) != 1 || len(endpointStatus.Results[0].ConditionResults) != 2 {
		t.Fatalf("expected 1 result with 2 condition results, got %+v", endpointStatus.Results)
	}
	details := endpointStatus.Results[0].ConditionResults[0].Details
	expected := []endpoint.ConditionResultDetail{{Label: "Attempt", Value: "WARP · HTTP 403"}, {Label: "Attempt", Value: "direct · HTTP 200"}, {Label: "Reason", Value: ""}}
	if len(details) != len(expected) {
		t.Fatalf("expected %d details in pushed order, got %d", len(expected), len(details))
	}
	for i, detail := range details {
		if *detail != expected[i] {
			t.Errorf("expected detail %d to be %+v, got %+v", i, expected[i], *detail)
		}
	}
	if endpointStatus.Results[0].ConditionResults[1].Details != nil {
		t.Errorf("expected a condition result pushed without details to have none, got %+v", endpointStatus.Results[0].ConditionResults[1].Details)
	}
}

func TestCreateExternalEndpointResultWithHostname(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	cfg := &config.Config{
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:  "n",
				Group: "g",
				Token: "token",
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	for _, path := range []string{
		"/api/v1/endpoints/g_n/external?success=true&hostname=example.org",
		"/api/v1/endpoints/g_n/external?success=true",
	} {
		request := httptest.NewRequest("POST", path, http.NoBody)
		request.Header.Set("Authorization", "Bearer token")
		response, err := router.Test(request)
		if err != nil {
			t.Fatalf("failed to push result: %s", err.Error())
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s %s should have returned 200, but returned %d instead", request.Method, request.URL, response.StatusCode)
		}
	}
	endpointStatus, err := store.Get().GetEndpointStatusByKey("g_n", paging.NewEndpointStatusParams().WithResults(1, 10))
	if err != nil {
		t.Fatalf("failed to get endpoint status: %s", err.Error())
	}
	if len(endpointStatus.Results) != 2 {
		t.Fatalf("expected 2 results but got %d", len(endpointStatus.Results))
	}
	if endpointStatus.Results[0].Hostname != "example.org" {
		t.Errorf("expected first result to have hostname example.org but got '%s'", endpointStatus.Results[0].Hostname)
	}
	if endpointStatus.Results[1].Hostname != "" {
		t.Errorf("expected second result to have no hostname, because none was pushed, but got '%s'", endpointStatus.Results[1].Hostname)
	}
}

func TestCreateExternalEndpointResultWithLogo(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	defer client.InjectHTTPClient(nil)
	var alertBody string
	client.InjectHTTPClient(&http.Client{Transport: test.MockRoundTripper(func(r *http.Request) *http.Response {
		body, _ := io.ReadAll(r.Body)
		alertBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
	})})
	cfg := &config.Config{
		Alerting: &alerting.Config{
			Discord: &discord.AlertProvider{DefaultConfig: discord.Config{WebhookURL: "https://discord.example.org", ComponentsV2: true}},
		},
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:   "n",
				Group:  "g",
				Token:  "token",
				Alerts: []*alert.Alert{{Type: alert.TypeDiscord, FailureThreshold: 1, SuccessThreshold: 1}},
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	request := httptest.NewRequest("POST", "/api/v1/endpoints/g_n/external?success=false&logo="+url.QueryEscape("https://example.org/logo.png"), http.NoBody)
	request.Header.Set("Authorization", "Bearer token")
	response, err := router.Test(request)
	if err != nil {
		t.Fatalf("failed to push result: %s", err.Error())
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("%s %s should have returned 200, but returned %d instead", request.Method, request.URL, response.StatusCode)
	}
	if !strings.Contains(alertBody, `"media":{"url":"https://example.org/logo.png"}`) {
		t.Errorf("expected the alert to carry the pushed logo as a thumbnail, got body: %s", alertBody)
	}
}

func TestCreateExternalEndpointResultWithExplicitKey(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	cfg := &config.Config{
		ExternalEndpoints: []*endpoint.ExternalEndpoint{
			{
				Name:        "A Human Readable Name",
				Group:       "some-group",
				ExplicitKey: "some-stable-uuid",
				Token:       "token",
			},
		},
		Maintenance: &maintenance.Config{},
	}
	api := New(cfg)
	router := api.Router()
	request := httptest.NewRequest("POST", "/api/v1/endpoints/some-stable-uuid/external?success=true", http.NoBody)
	request.Header.Set("Authorization", "Bearer token")
	response, err := router.Test(request)
	if err != nil {
		t.Fatalf("failed to push result: %s", err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("expected push to return 200, but returned %d instead", response.StatusCode)
	}
	// The result must be stored under the explicit key.
	endpointStatus, err := store.Get().GetEndpointStatusByKey("some-stable-uuid", paging.NewEndpointStatusParams().WithResults(1, 1))
	if err != nil {
		t.Fatalf("failed to get endpoint status by explicit key: %s", err.Error())
	}
	if endpointStatus.Key != "some-stable-uuid" {
		t.Errorf("expected stored key to be some-stable-uuid but got %s", endpointStatus.Key)
	}
	if len(endpointStatus.Results) != 1 || !endpointStatus.Results[0].Success {
		t.Errorf("expected exactly one successful result under the explicit key")
	}
	// It must NOT be stored under the derived group_name key.
	if derived, _ := store.Get().GetEndpointStatusByKey("some-group_a-human-readable-name", paging.NewEndpointStatusParams().WithResults(1, 1)); derived != nil && len(derived.Results) > 0 {
		t.Errorf("result should not have been stored under the derived key")
	}
}
