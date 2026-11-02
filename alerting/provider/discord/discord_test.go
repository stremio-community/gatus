package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/TwiN/gatus/v5/alerting/alert"
	"github.com/TwiN/gatus/v5/client"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"github.com/TwiN/gatus/v5/test"
)

func TestAlertProvider_Validate(t *testing.T) {
	invalidProvider := AlertProvider{DefaultConfig: Config{WebhookURL: ""}}
	if err := invalidProvider.Validate(); err == nil {
		t.Error("provider shouldn't have been valid")
	}
	validProvider := AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}}
	if err := validProvider.Validate(); err != nil {
		t.Error("provider should've been valid")
	}
}

func TestAlertProvider_ValidateWithOverride(t *testing.T) {
	providerWithInvalidOverrideGroup := AlertProvider{
		Overrides: []Override{
			{
				Config: Config{WebhookURL: "http://example.com"},
				Group:  "",
			},
		},
	}
	if err := providerWithInvalidOverrideGroup.Validate(); err == nil {
		t.Error("provider Group shouldn't have been valid")
	}
	providerWithInvalidOverrideTo := AlertProvider{
		Overrides: []Override{
			{
				Config: Config{WebhookURL: ""},
				Group:  "group",
			},
		},
	}
	if err := providerWithInvalidOverrideTo.Validate(); err == nil {
		t.Error("provider integration key shouldn't have been valid")
	}
	providerWithValidOverride := AlertProvider{
		DefaultConfig: Config{
			WebhookURL: "http://example.com",
		},
		Overrides: []Override{
			{
				Config: Config{WebhookURL: "http://example.com"},
				Group:  "group",
			},
		},
	}
	if err := providerWithValidOverride.Validate(); err != nil {
		t.Error("provider should've been valid")
	}
}

func TestAlertProvider_Send(t *testing.T) {
	defer client.InjectHTTPClient(nil)
	firstDescription := "description-1"
	secondDescription := "description-2"
	title := "provider-title"
	scenarios := []struct {
		Name             string
		Provider         AlertProvider
		Alert            alert.Alert
		Resolved         bool
		MockRoundTripper test.MockRoundTripper
		ExpectedError    bool
	}{
		{
			Name:     "triggered",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "triggered-error",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}
			}),
			ExpectedError: true,
		},
		{
			Name:     "resolved",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: true,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "resolved-error",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: true,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}
			}),
			ExpectedError: true,
		},
		{
			Name:     "triggered-with-modified-title",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com", Title: title}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "triggered-with-webhook-override",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3, ProviderOverride: map[string]any{"webhook-url": "http://example01.com"}},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "triggered-with-message-content",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com", MessageContent: "<@123456789>"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			client.InjectHTTPClient(&http.Client{Transport: scenario.MockRoundTripper})
			err := scenario.Provider.Send(
				&endpoint.Endpoint{Name: "endpoint-name"},
				&scenario.Alert,
				&endpoint.Result{
					ConditionResults: []*endpoint.ConditionResult{
						{Condition: "[CONNECTED] == true", Success: scenario.Resolved},
						{Condition: "[STATUS] == 200", Success: scenario.Resolved},
					},
				},
				scenario.Resolved,
			)
			if scenario.ExpectedError && err == nil {
				t.Error("expected error, got none")
			}
			if !scenario.ExpectedError && err != nil {
				t.Error("expected no error, got", err.Error())
			}
		})
	}
}

func TestAlertProvider_buildRequestBody(t *testing.T) {
	firstDescription := "description-1"
	secondDescription := "description-2"
	title := "provider-title"
	scenarios := []struct {
		Name         string
		Provider     AlertProvider
		Alert        alert.Alert
		NoConditions bool
		Resolved     bool
		ExpectedBody string
	}{
		{
			Name:         "triggered",
			Provider:     AlertProvider{},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"content\":\"\",\"embeds\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"description\":\"An alert for **endpoint-name** has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"color\":15158332,\"fields\":[{\"name\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n:x: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
		{
			Name:         "resolved",
			Provider:     AlertProvider{},
			Alert:        alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     true,
			ExpectedBody: "{\"content\":\"\",\"embeds\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"description\":\"An alert for **endpoint-name** has been resolved after passing successfully 5 time(s) in a row:\\n\\u003e description-2\",\"color\":3066993,\"fields\":[{\"name\":\"Condition results\",\"value\":\":white_check_mark: - `[CONNECTED] == true`\\n:white_check_mark: - `[STATUS] == 200`\\n:white_check_mark: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
		{
			Name:         "triggered-with-modified-title",
			Provider:     AlertProvider{DefaultConfig: Config{Title: title}},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"content\":\"\",\"embeds\":[{\"title\":\"provider-title\",\"description\":\"An alert for **endpoint-name** has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"color\":15158332,\"fields\":[{\"name\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n:x: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
		{
			Name:         "triggered-with-no-conditions",
			NoConditions: true,
			Provider:     AlertProvider{DefaultConfig: Config{Title: title}},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"content\":\"\",\"embeds\":[{\"title\":\"provider-title\",\"description\":\"An alert for **endpoint-name** has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"color\":15158332}]}",
		},
		{
			Name:         "triggered-with-message-content-user-mention",
			Provider:     AlertProvider{DefaultConfig: Config{MessageContent: "<@123456789>"}},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"content\":\"\\u003c@123456789\\u003e\",\"embeds\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"description\":\"An alert for **endpoint-name** has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"color\":15158332,\"fields\":[{\"name\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n:x: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
		{
			Name:         "triggered-with-message-content-role-mention",
			Provider:     AlertProvider{DefaultConfig: Config{MessageContent: "<@&987654321>"}},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"content\":\"\\u003c@\\u0026987654321\\u003e\",\"embeds\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"description\":\"An alert for **endpoint-name** has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"color\":15158332,\"fields\":[{\"name\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n:x: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
		{
			Name:         "resolved-with-message-content",
			Provider:     AlertProvider{DefaultConfig: Config{MessageContent: "<@123456789>"}},
			Alert:        alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     true,
			ExpectedBody: "{\"content\":\"\\u003c@123456789\\u003e\",\"embeds\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"description\":\"An alert for **endpoint-name** has been resolved after passing successfully 5 time(s) in a row:\\n\\u003e description-2\",\"color\":3066993,\"fields\":[{\"name\":\"Condition results\",\"value\":\":white_check_mark: - `[CONNECTED] == true`\\n:white_check_mark: - `[STATUS] == 200`\\n:white_check_mark: - `[BODY] != \\\"\\\"`\\n\",\"inline\":false}]}]}",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			var conditionResults []*endpoint.ConditionResult
			if !scenario.NoConditions {
				conditionResults = []*endpoint.ConditionResult{
					{Condition: "[CONNECTED] == true", Success: scenario.Resolved},
					{Condition: "[STATUS] == 200", Success: scenario.Resolved},
					{Condition: "[BODY] != \"\"", Success: scenario.Resolved},
				}
			}
			body := scenario.Provider.buildRequestBody(
				&scenario.Provider.DefaultConfig,
				&endpoint.Endpoint{Name: "endpoint-name"},
				&scenario.Alert,
				&endpoint.Result{
					ConditionResults: conditionResults,
				},
				scenario.Resolved,
			)
			if string(body) != scenario.ExpectedBody {
				t.Errorf("expected:\n%s\ngot:\n%s", scenario.ExpectedBody, body)
			}
			out := make(map[string]interface{})
			if err := json.Unmarshal(body, &out); err != nil {
				t.Error("expected body to be valid JSON, got error:", err.Error())
			}
		})
	}
}

func TestAlertProvider_buildRequestBodyWithComponentsV2(t *testing.T) {
	description := "description-1"
	timestamp := time.Unix(1700000000, 0)
	failing := []*endpoint.ConditionResult{
		{Condition: "[CONNECTED] == true", Success: true},
		{Condition: "[STATUS] == 200", Success: false},
		{Condition: "[BODY] != \"\"", Success: false},
	}
	scenarios := []struct {
		Name             string
		Config           Config
		Endpoint         endpoint.Endpoint
		Alert            alert.Alert
		ConditionResults []*endpoint.ConditionResult
		Hostname         string
		Duration         time.Duration
		Logo             string
		Timestamp        time.Time
		Resolved         bool
		ExpectedBody     string
	}{
		{
			Name:             "triggered",
			Config:           Config{ComponentsV2: true, Title: "ignored"},
			Endpoint:         endpoint.Endpoint{Name: "endpoint-name", Group: "  group"},
			Alert:            alert.Alert{Description: &description},
			ConditionResults: failing,
			Hostname:         "example.org",
			Timestamp:        timestamp,
			ExpectedBody:     `{"flags":32768,"components":[{"type":17,"accent_color":15158332,"components":[{"type":10,"content":"**endpoint-name** · group"},{"type":10,"content":"-# description-1 · \u003ct:1700000000:R\u003e"},{"type":10,"content":"❌ ` + "`[STATUS] == 200`" + `\n❌ ` + "`[BODY] != \\\"\\\"`" + `\n✅ ` + "`[CONNECTED] == true`" + `"}]}]}`,
		},
		{
			Name:     "triggered-with-duration-and-details",
			Config:   Config{ComponentsV2: true, DetailLabels: []string{"Reason", "Test ID"}},
			Endpoint: endpoint.Endpoint{Name: "endpoint-name"},
			ConditionResults: []*endpoint.ConditionResult{
				{Condition: "manifest: reachable", Success: true, Details: []*endpoint.ConditionResultDetail{{Label: "Reason", Value: "not shown under a passing condition"}}},
				{Condition: "stream: 0 of 5 valid", Success: false, Details: []*endpoint.ConditionResultDetail{
					{Label: "Request", Value: "/stream/movie/tt1.json"},
					{Label: "Test ID", Value: "tt1"},
					{Label: "Reason", Value: "HTTP `500`\nupstream"},
				}},
			},
			Hostname:     "example.org",
			Duration:     1434 * time.Millisecond,
			Timestamp:    timestamp,
			ExpectedBody: `{"flags":32768,"components":[{"type":17,"accent_color":15158332,"components":[{"type":10,"content":"**endpoint-name**"},{"type":10,"content":"-# example.org · 1.43s · \u003ct:1700000000:R\u003e"},{"type":10,"content":"❌ ` + "`stream: 0 of 5 valid`" + `\n-# Test ID: ` + "`tt1`" + `\n-# Reason: ` + "`HTTP '500' upstream`" + `\n✅ ` + "`manifest: reachable`" + `"}]}]}`,
		},
		{
			Name:             "resolved",
			Config:           Config{ComponentsV2: true},
			Endpoint:         endpoint.Endpoint{Name: "endpoint-name"},
			ConditionResults: []*endpoint.ConditionResult{{Condition: "[STATUS] == 200", Success: true}},
			Hostname:         "example.org",
			Timestamp:        timestamp,
			Resolved:         true,
			ExpectedBody:     `{"flags":32768,"components":[{"type":17,"accent_color":3066993,"components":[{"type":10,"content":"**endpoint-name**"},{"type":10,"content":"-# example.org · \u003ct:1700000000:R\u003e"}]}]}`,
		},
		{
			Name:             "triggered-without-subtext",
			Config:           Config{ComponentsV2: true},
			Endpoint:         endpoint.Endpoint{Name: "endpoint-name"},
			ConditionResults: []*endpoint.ConditionResult{{Condition: "[STATUS] == 200", Success: false}},
			ExpectedBody:     `{"flags":32768,"components":[{"type":17,"accent_color":15158332,"components":[{"type":10,"content":"**endpoint-name**"},{"type":10,"content":"❌ ` + "`[STATUS] == 200`" + `"}]}]}`,
		},
		{
			Name:             "triggered-with-logo",
			Config:           Config{ComponentsV2: true},
			Endpoint:         endpoint.Endpoint{Name: "endpoint-name"},
			ConditionResults: []*endpoint.ConditionResult{{Condition: "[STATUS] == 200", Success: false}},
			Hostname:         "example.org",
			Logo:             "https://example.org/logo.png",
			Timestamp:        timestamp,
			ExpectedBody:     `{"flags":32768,"components":[{"type":17,"accent_color":15158332,"components":[{"type":9,"components":[{"type":10,"content":"**endpoint-name**"},{"type":10,"content":"-# example.org · \u003ct:1700000000:R\u003e"},{"type":10,"content":"` + thumbnailAlignmentPadding + `"}],"accessory":{"type":11,"media":{"url":"https://example.org/logo.png"}}},{"type":10,"content":"❌ ` + "`[STATUS] == 200`" + `"}]}]}`,
		},
		{
			Name:         "triggered-with-message-content-and-status-page-url",
			Config:       Config{ComponentsV2: true, MessageContent: "<@123456789>", StatusPageURL: "https://status.example.org/"},
			Endpoint:     endpoint.Endpoint{Name: "endpoint-name", Group: "group"},
			ExpectedBody: `{"flags":32768,"components":[{"type":10,"content":"\u003c@123456789\u003e"},{"type":17,"accent_color":15158332,"components":[{"type":10,"content":"**endpoint-name** · group"},{"type":1,"components":[{"type":2,"style":5,"label":"View on status page","url":"https://status.example.org/endpoints/group_endpoint-name"}]}]}]}`,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			body := (&AlertProvider{}).buildRequestBody(
				&scenario.Config,
				&scenario.Endpoint,
				&scenario.Alert,
				&endpoint.Result{ConditionResults: scenario.ConditionResults, Hostname: scenario.Hostname, Duration: scenario.Duration, Logo: scenario.Logo, Timestamp: scenario.Timestamp},
				scenario.Resolved,
			)
			if string(body) != scenario.ExpectedBody {
				t.Errorf("expected:\n%s\ngot:\n%s", scenario.ExpectedBody, body)
			}
		})
	}
}

func TestAlertProvider_buildRequestBodyWithComponentsV2CapsConditions(t *testing.T) {
	var conditionResults []*endpoint.ConditionResult
	for i := 0; i < maximumConditionLines+3; i++ {
		conditionResults = append(conditionResults, &endpoint.ConditionResult{Condition: fmt.Sprintf("[STATUS] == %d", i), Success: false})
	}
	var body ComponentsV2Body
	raw := (&AlertProvider{}).buildRequestBody(&Config{ComponentsV2: true}, &endpoint.Endpoint{Name: "endpoint-name"}, &alert.Alert{}, &endpoint.Result{ConditionResults: conditionResults}, false)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal("expected body to be valid JSON, got error:", err.Error())
	}
	lines := strings.Split(body.Components[0].Components[1].Content, "\n")
	if len(lines) != maximumConditionLines+1 {
		t.Fatalf("expected %d lines, got %d", maximumConditionLines+1, len(lines))
	}
	if last := lines[len(lines)-1]; last != "+3 more" {
		t.Errorf("expected last line to be %q, got %q", "+3 more", last)
	}
}

func TestFormatConditions_capsDetailsPerCondition(t *testing.T) {
	var details []*endpoint.ConditionResultDetail
	for i := 0; i < maximumDetailLinesPerCondition+2; i++ {
		details = append(details, &endpoint.ConditionResultDetail{Label: "Attempt", Value: fmt.Sprintf("attempt %d", i)})
	}
	got := formatConditions(&endpoint.Result{ConditionResults: []*endpoint.ConditionResult{{Condition: "stream: unreachable", Details: details}}}, []string{"Attempt"})
	lines := strings.Split(got, "\n")
	if len(lines) != 1+maximumDetailLinesPerCondition+1 {
		t.Fatalf("expected %d lines, got %d:\n%s", 1+maximumDetailLinesPerCondition+1, len(lines), got)
	}
	if last := lines[len(lines)-1]; last != "-# +2 more" {
		t.Errorf("expected last line to be %q, got %q", "-# +2 more", last)
	}
}

func TestFormatConditions_staysWithinCharacterBudget(t *testing.T) {
	long := strings.Repeat("x", 512)
	var conditionResults []*endpoint.ConditionResult
	for i := 0; i < maximumConditionLines; i++ {
		var details []*endpoint.ConditionResultDetail
		for j := 0; j < maximumDetailLinesPerCondition; j++ {
			details = append(details, &endpoint.ConditionResultDetail{Label: "Reason", Value: long})
		}
		conditionResults = append(conditionResults, &endpoint.ConditionResult{Condition: fmt.Sprintf("condition %d", i), Details: details})
	}
	got := formatConditions(&endpoint.Result{ConditionResults: conditionResults}, []string{"Reason"})
	if length := utf8.RuneCountInString(got); length > conditionsCharacterBudget+len("\n+10 more") {
		t.Errorf("expected at most %d characters, got %d", conditionsCharacterBudget, length)
	}
	if !strings.Contains(got, "…`") {
		t.Error("expected long detail values to be truncated")
	}
	if !strings.HasSuffix(got, " more") {
		t.Error("expected the conditions past the budget to be counted as omitted")
	}
}

func TestAlertProvider_SendWithRejectedLogo(t *testing.T) {
	defer client.InjectHTTPClient(nil)
	var bodies []string
	client.InjectHTTPClient(&http.Client{Transport: test.MockRoundTripper(func(r *http.Request) *http.Response {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if strings.Contains(string(body), `"accessory"`) {
			return &http.Response{StatusCode: http.StatusBadRequest, Body: http.NoBody}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
	})})
	provider := AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com", ComponentsV2: true}}
	err := provider.Send(&endpoint.Endpoint{Name: "endpoint-name"}, &alert.Alert{}, &endpoint.Result{Logo: "https://example.org/logo.png"}, false)
	if err != nil {
		t.Fatal("expected the alert to be sent without its logo, got error:", err.Error())
	}
	if len(bodies) != 2 || strings.Contains(bodies[1], `"accessory"`) {
		t.Errorf("expected a retry without the thumbnail, got bodies: %v", bodies)
	}
}

func TestConfig_requestURL(t *testing.T) {
	scenarios := []struct {
		Name     string
		Config   Config
		Expected string
	}{
		{Name: "embed", Config: Config{WebhookURL: "https://discord.com/api/webhooks/1/token"}, Expected: "https://discord.com/api/webhooks/1/token"},
		{Name: "components-v2", Config: Config{WebhookURL: "https://discord.com/api/webhooks/1/token", ComponentsV2: true}, Expected: "https://discord.com/api/webhooks/1/token?with_components=true"},
		{Name: "components-v2-with-query", Config: Config{WebhookURL: "https://discord.com/api/webhooks/1/token?thread_id=2", ComponentsV2: true}, Expected: "https://discord.com/api/webhooks/1/token?thread_id=2&with_components=true"},
		{Name: "components-v2-already-set", Config: Config{WebhookURL: "https://discord.com/api/webhooks/1/token?with_components=true", ComponentsV2: true}, Expected: "https://discord.com/api/webhooks/1/token?with_components=true"},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			if got := scenario.Config.requestURL(); got != scenario.Expected {
				t.Errorf("expected %s, got %s", scenario.Expected, got)
			}
		})
	}
}

func TestAlertProvider_GetDefaultAlert(t *testing.T) {
	if (&AlertProvider{DefaultAlert: &alert.Alert{}}).GetDefaultAlert() == nil {
		t.Error("expected default alert to be not nil")
	}
	if (&AlertProvider{DefaultAlert: nil}).GetDefaultAlert() != nil {
		t.Error("expected default alert to be nil")
	}
}

func TestAlertProvider_GetConfig(t *testing.T) {
	scenarios := []struct {
		Name           string
		Provider       AlertProvider
		InputGroup     string
		InputAlert     alert.Alert
		ExpectedOutput Config
	}{
		{
			Name: "provider-no-override-specify-no-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides:     nil,
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-no-override-specify-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides:     nil,
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-with-override-specify-no-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com"},
					},
				},
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-with-override-specify-group-should-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://group-example.com"},
		},
		{
			Name: "provider-with-group-override-and-alert-override--alert-override-should-take-precedence",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{ProviderOverride: map[string]any{"webhook-url": "http://alert-example.com"}},
			ExpectedOutput: Config{WebhookURL: "http://alert-example.com"},
		},
		{
			Name: "provider-with-message-content-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com", MessageContent: "<@123456789>"},
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com", MessageContent: "<@123456789>"},
		},
		{
			Name: "provider-with-message-content-group-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com", MessageContent: "<@123456789>"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com", MessageContent: "<@&987654321>"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://group-example.com", MessageContent: "<@&987654321>"},
		},
		{
			Name: "provider-with-message-content-alert-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com", MessageContent: "<@123456789>"},
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{ProviderOverride: map[string]any{"message-content": "<@999999999>"}},
			ExpectedOutput: Config{WebhookURL: "http://example.com", MessageContent: "<@999999999>"},
		},
		{
			Name: "provider-with-components-v2-group-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com", ComponentsV2: true, StatusPageURL: "https://status.example.org"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://group-example.com", ComponentsV2: true, StatusPageURL: "https://status.example.org"},
		},
		{
			Name: "provider-with-detail-labels-alert-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com", DetailLabels: []string{"Reason"}},
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{ProviderOverride: map[string]any{"detail-labels": []string{"Reason", "Test ID"}}},
			ExpectedOutput: Config{WebhookURL: "http://example.com", DetailLabels: []string{"Reason", "Test ID"}},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			got, err := scenario.Provider.GetConfig(scenario.InputGroup, &scenario.InputAlert)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got.WebhookURL != scenario.ExpectedOutput.WebhookURL {
				t.Errorf("expected webhook URL to be %s, got %s", scenario.ExpectedOutput.WebhookURL, got.WebhookURL)
			}
			if got.MessageContent != scenario.ExpectedOutput.MessageContent {
				t.Errorf("expected message content to be %s, got %s", scenario.ExpectedOutput.MessageContent, got.MessageContent)
			}
			if got.ComponentsV2 != scenario.ExpectedOutput.ComponentsV2 {
				t.Errorf("expected components-v2 to be %t, got %t", scenario.ExpectedOutput.ComponentsV2, got.ComponentsV2)
			}
			if got.StatusPageURL != scenario.ExpectedOutput.StatusPageURL {
				t.Errorf("expected status page URL to be %s, got %s", scenario.ExpectedOutput.StatusPageURL, got.StatusPageURL)
			}
			if !slices.Equal(got.DetailLabels, scenario.ExpectedOutput.DetailLabels) {
				t.Errorf("expected detail labels to be %v, got %v", scenario.ExpectedOutput.DetailLabels, got.DetailLabels)
			}
			// Test ValidateOverrides as well, since it really just calls GetConfig
			if err = scenario.Provider.ValidateOverrides(scenario.InputGroup, &scenario.InputAlert); err != nil {
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}
